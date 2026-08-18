/**
 * patches_win.c — Détection et installation des mises à jour Windows via
 * l'API COM Windows Update Agent (WUA), même mécanisme que l'onglet
 * "Mises à jour Windows" du Panneau de configuration / "wuauclt" — pas de
 * dépendance à un module externe (contrairement à PSWindowsUpdate, qui
 * n'est pas installé par défaut).
 *
 * Recherche : IUpdateSession → IUpdateSearcher::Search("IsInstalled=0 and
 * IsHidden=0") → IUpdateCollection de IUpdate. Chaque IUpdate expose Title/
 * MsrcSeverity/MaxDownloadSize/KBArticleIDs.
 *
 * Installation : reconstruit une IUpdateCollection ne contenant que les
 * mises à jour dont l'id (premier KB, ou UpdateID en repli — voir
 * _fill_patch_from_update) correspond à la sélection demandée, puis
 * IUpdateDownloader::Download() suivi de IUpdateInstaller::Install()
 * (synchrones — pas de Begin/End, ce module tourne déjà dans un thread
 * dédié côté agent.c).
 *
 * Chaque appel public (re)initialise COM sur le thread appelant
 * (CoInitializeEx / CoUninitialize) : ce module est invoqué depuis un
 * thread détaché par commande (voir _patch_thread / _EXEC_KIND_INSTALL_PATCHES
 * dans agent.c), jamais depuis le thread WSS, et chaque thread COM a besoin
 * de sa propre initialisation d'appartement.
 */
#include "../../src/patches.h"
#include "../../src/logger.h"

/* INITGUID : ce fichier est le seul du projet à inclure wuapi.h — sans ce
 * define, DEFINE_GUID() se contente d'un "extern const GUID ..." (aucune
 * définition), et CLSID_UpdateSession/IID_IUpdateSession/IID_IUpdateCollection
 * échouent à l'édition de liens ("undefined reference"). DECLSPEC_SELECTANY
 * rend la définition résultante sûre même incluse depuis plusieurs unités de
 * compilation (comdat dédupliqué par l'éditeur de liens), donc pas besoin de
 * restreindre ce define à un seul fichier "guid.c" séparé. */
#define INITGUID
#define COBJMACROS
#include <objbase.h>
#include <oleauto.h>
#include <wuapi.h>

#include <stdio.h>
#include <string.h>

/* Critère de recherche standard des échantillons WUA officiels : mises à
 * jour ni déjà installées ni masquées par l'utilisateur/l'administrateur. */
#define _SEARCH_CRITERIA  L"IsInstalled=0 and IsHidden=0"

/* ─── Conversions / helpers ──────────────────────────────────────────────── */

static void _bstr_to_utf8(BSTR src, char *out, size_t out_sz) {
    out[0] = '\0';
    if (!src || out_sz == 0) return;
    WideCharToMultiByte(CP_UTF8, 0, src, -1, out, (int)out_sz, NULL, NULL);
    out[out_sz - 1] = '\0';
}

/** MsrcSeverity ("Critical"/"Important"/"Moderate"/"Low"/"") → sévérité
 *  interne. Une valeur absente (patch sans classification MSRC — fréquent
 *  pour les mises à jour de pilotes/définitions) est traitée "important"
 *  par prudence plutôt que reléguée à "optional". */
static leo_patch_severity_t _severity_from_msrc(BSTR msrc) {
    char buf[32];
    _bstr_to_utf8(msrc, buf, sizeof(buf));
    if (buf[0] == '\0')                     return LEO_PATCH_SEVERITY_IMPORTANT;
    if (_stricmp(buf, "Critical") == 0)     return LEO_PATCH_SEVERITY_CRITICAL;
    if (_stricmp(buf, "Important") == 0)    return LEO_PATCH_SEVERITY_IMPORTANT;
    return LEO_PATCH_SEVERITY_OPTIONAL;     /* "Moderate", "Low", ou valeur inconnue */
}

/** Remplit out à partir d'un IUpdate — titre, sévérité, taille, et un
 *  identifiant stable : le premier KB connu ("KB1234567"), ou à défaut
 *  l'UpdateID (GUID) exposé par IUpdateIdentity. Laisse out->id vide si ni
 *  l'un ni l'autre n'est disponible (l'appelant ignore alors cette entrée). */
static void _fill_patch_from_update(IUpdate *update, leo_patch_t *out) {
    memset(out, 0, sizeof(*out));

    BSTR title = NULL;
    if (SUCCEEDED(IUpdate_get_Title(update, &title)) && title) {
        _bstr_to_utf8(title, out->title, sizeof(out->title));
        SysFreeString(title);
    }

    BSTR msrc = NULL;
    IUpdate_get_MsrcSeverity(update, &msrc);
    out->severity = _severity_from_msrc(msrc);
    if (msrc) SysFreeString(msrc);

    DECIMAL dec_size;
    if (SUCCEEDED(IUpdate_get_MaxDownloadSize(update, &dec_size))) {
        double d = 0.0;
        if (SUCCEEDED(VarR8FromDec(&dec_size, &d)) && d > 0)
            out->size_bytes = (uint64_t)d;
    }

    IStringCollection *kbs = NULL;
    if (SUCCEEDED(IUpdate_get_KBArticleIDs(update, &kbs)) && kbs) {
        LONG count = 0;
        IStringCollection_get_Count(kbs, &count);
        if (count > 0) {
            BSTR kb = NULL;
            if (SUCCEEDED(IStringCollection_get_Item(kbs, 0, &kb)) && kb) {
                char kbbuf[32];
                _bstr_to_utf8(kb, kbbuf, sizeof(kbbuf));
                snprintf(out->id, sizeof(out->id), "KB%s", kbbuf);
                SysFreeString(kb);
            }
        }
        IStringCollection_Release(kbs);
    }

    if (out->id[0] == '\0') {
        IUpdateIdentity *identity = NULL;
        if (SUCCEEDED(IUpdate_get_Identity(update, &identity)) && identity) {
            BSTR uid = NULL;
            if (SUCCEEDED(IUpdateIdentity_get_UpdateID(identity, &uid)) && uid) {
                _bstr_to_utf8(uid, out->id, sizeof(out->id));
                SysFreeString(uid);
            }
            IUpdateIdentity_Release(identity);
        }
    }
}

/* ─── Session / recherche ────────────────────────────────────────────────── */

static IUpdateSession *_create_session(void) {
    IUpdateSession *session = NULL;
    HRESULT hr = CoCreateInstance(&CLSID_UpdateSession, NULL, CLSCTX_INPROC_SERVER,
                                   &IID_IUpdateSession, (void **)&session);
    if (FAILED(hr)) {
        LOG_WARN("CoCreateInstance(UpdateSession) a échoué (hr=0x%08lx)", (unsigned long)hr);
        return NULL;
    }
    return session;
}

/** Lance une recherche synchrone. *out_searcher reçoit le IUpdateSearcher
 *  créé (à libérer par l'appelant avec le résultat), ou reste NULL en cas
 *  d'échec. */
static ISearchResult *_search(IUpdateSession *session, IUpdateSearcher **out_searcher) {
    *out_searcher = NULL;

    IUpdateSearcher *searcher = NULL;
    if (FAILED(IUpdateSession_CreateUpdateSearcher(session, &searcher)) || !searcher) {
        LOG_WARN("CreateUpdateSearcher a échoué");
        return NULL;
    }

    BSTR criteria = SysAllocString(_SEARCH_CRITERIA);
    ISearchResult *result = NULL;
    HRESULT hr = IUpdateSearcher_Search(searcher, criteria, &result);
    if (criteria) SysFreeString(criteria);

    if (FAILED(hr) || !result) {
        LOG_WARN("IUpdateSearcher::Search a échoué (hr=0x%08lx)", (unsigned long)hr);
        IUpdateSearcher_Release(searcher);
        return NULL;
    }

    *out_searcher = searcher;
    return result;
}

/* ─── Collecte (LEO_MSG_PATCH_INVENTORY) ────────────────────────────────── */

int leo_patches_collect(leo_patch_t *out, int max_items) {
    if (!out || max_items <= 0) return -1;

    HRESULT hr_init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    /* RPC_E_CHANGED_MODE : appartement déjà initialisé avec un autre modèle
     * sur ce thread (improbable ici — un thread dédié par commande — mais
     * pas fatal : les appels COM restent utilisables). */
    if (FAILED(hr_init) && hr_init != RPC_E_CHANGED_MODE) {
        LOG_WARN("CoInitializeEx a échoué (hr=0x%08lx) — collecte des patchs abandonnée",
                 (unsigned long)hr_init);
        return 0;
    }
    bool need_uninit = SUCCEEDED(hr_init);

    int n = 0;
    IUpdateSession *session = _create_session();
    if (session) {
        IUpdateSearcher *searcher = NULL;
        ISearchResult *result = _search(session, &searcher);
        if (result) {
            IUpdateCollection *updates = NULL;
            if (SUCCEEDED(ISearchResult_get_Updates(result, &updates)) && updates) {
                LONG count = 0;
                IUpdateCollection_get_Count(updates, &count);
                for (LONG i = 0; i < count && n < max_items; i++) {
                    IUpdate *upd = NULL;
                    if (SUCCEEDED(IUpdateCollection_get_Item(updates, i, &upd)) && upd) {
                        _fill_patch_from_update(upd, &out[n]);
                        if (out[n].id[0] != '\0') n++;
                        IUpdate_Release(upd);
                    }
                }
                IUpdateCollection_Release(updates);
            }
            ISearchResult_Release(result);
        }
        if (searcher) IUpdateSearcher_Release(searcher);
        IUpdateSession_Release(session);
    }

    if (need_uninit) CoUninitialize();
    LOG_DEBUG("Patchs disponibles (Windows Update) : %d", n);
    return n;
}

/* ─── Installation (LEO_MSG_INSTALL_PATCHES) ────────────────────────────── */

static IUpdateCollection *_create_update_collection(void) {
    /* Pas de CLSID_UpdateColl exposé par wuapi.h (les collections
     * "libres" — hors résultat de recherche — n'existent que via ce
     * ProgID, enregistré par l'agent Windows Update lui-même) : c'est le
     * chemin documenté par les échantillons C++/VBScript officiels de la
     * WUA SDK pour construire une IUpdateCollection à passer à
     * IUpdateDownloader/IUpdateInstaller. */
    CLSID clsid;
    if (FAILED(CLSIDFromProgID(L"Microsoft.Update.UpdateColl", &clsid))) {
        LOG_WARN("CLSIDFromProgID(Microsoft.Update.UpdateColl) a échoué");
        return NULL;
    }
    IUpdateCollection *coll = NULL;
    HRESULT hr = CoCreateInstance(&clsid, NULL, CLSCTX_INPROC_SERVER,
                                   &IID_IUpdateCollection, (void **)&coll);
    if (FAILED(hr)) {
        LOG_WARN("CoCreateInstance(Microsoft.Update.UpdateColl) a échoué (hr=0x%08lx)", (unsigned long)hr);
        return NULL;
    }
    return coll;
}

static bool _id_in_list(const char *id, const char *const ids[], int count) {
    for (int i = 0; i < count; i++)
        if (strcmp(id, ids[i]) == 0) return true;
    return false;
}

/** Active SE_SHUTDOWN_NAME sur le token du processus courant — requis par
 *  InitiateSystemShutdownExW (l'agent tourne en service LocalSystem, qui
 *  possède le privilège mais ne l'a pas activé par défaut). Best-effort :
 *  un échec est logué mais ne bloque pas l'installation déjà réussie. */
static void _enable_shutdown_privilege(void) {
    HANDLE token;
    if (!OpenProcessToken(GetCurrentProcess(), TOKEN_ADJUST_PRIVILEGES | TOKEN_QUERY, &token)) {
        LOG_WARN("OpenProcessToken a échoué (%lu) — redémarrage planifié pourrait échouer",
                 (unsigned long)GetLastError());
        return;
    }

    TOKEN_PRIVILEGES tp;
    tp.PrivilegeCount = 1;
    tp.Privileges[0].Attributes = SE_PRIVILEGE_ENABLED;
    /* L"SeShutdownPrivilege" explicite, pas la macro SE_SHUTDOWN_NAME : elle
     * se résout en littéral ANSI (char*) sauf UNICODE défini, incompatible
     * avec LookupPrivilegeValueW (LPCWSTR) — voir aussi les autres appels
     * *W explicites de ce fichier. */
    if (!LookupPrivilegeValueW(NULL, L"SeShutdownPrivilege", &tp.Privileges[0].Luid)) {
        LOG_WARN("LookupPrivilegeValueW(SE_SHUTDOWN_NAME) a échoué (%lu)", (unsigned long)GetLastError());
        CloseHandle(token);
        return;
    }

    AdjustTokenPrivileges(token, FALSE, &tp, sizeof(tp), NULL, NULL);
    CloseHandle(token);
}

leo_error_t leo_patches_install(const char *const ids[], int count, bool reboot_after,
                                 int timeout_secs, leo_exec_result_t *result) {
    /* WUA n'expose pas de timeout par appel (Download()/Install() sont
     * synchrones et bloquent jusqu'à leur terme) — accepté pour la symétrie
     * d'interface avec patches_linux.c, sans effet ici. */
    (void)timeout_secs;
    if (!ids || count <= 0 || !result) return LEO_ERR_SYSTEM;
    memset(result, 0, sizeof(*result));
    result->exit_code = -1;  /* pessimiste par défaut, mis à 0 seulement au succès avéré */

    IUpdateSession       *session       = NULL;
    IUpdateSearcher       *searcher      = NULL;
    ISearchResult         *search_result = NULL;
    IUpdateCollection     *all_updates   = NULL;
    IUpdateCollection     *to_install    = NULL;
    IUpdateDownloader     *downloader    = NULL;
    IDownloadResult       *dl_result     = NULL;
    IUpdateInstaller      *installer     = NULL;
    IInstallationResult   *inst_result   = NULL;
    int matched = 0;

    HRESULT hr_init = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    if (FAILED(hr_init) && hr_init != RPC_E_CHANGED_MODE) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "CoInitializeEx a échoué (hr=0x%08lx)", (unsigned long)hr_init);
        return LEO_OK;
    }
    bool need_uninit = SUCCEEDED(hr_init);

    session = _create_session();
    if (!session) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Impossible de créer une session Windows Update");
        goto cleanup;
    }

    search_result = _search(session, &searcher);
    if (!search_result) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Échec de la recherche des mises à jour");
        goto cleanup;
    }

    ISearchResult_get_Updates(search_result, &all_updates);
    to_install = _create_update_collection();
    if (!all_updates || !to_install) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Impossible de préparer la liste des patchs à installer");
        goto cleanup;
    }

    {
        LONG total = 0;
        IUpdateCollection_get_Count(all_updates, &total);
        for (LONG i = 0; i < total; i++) {
            IUpdate *upd = NULL;
            if (FAILED(IUpdateCollection_get_Item(all_updates, i, &upd)) || !upd) continue;

            leo_patch_t tmp;
            _fill_patch_from_update(upd, &tmp);
            if (tmp.id[0] != '\0' && _id_in_list(tmp.id, ids, count)) {
                /* AcceptEula best-effort : un échec ici n'empêche pas
                 * Download()/Install() d'être tentés — ils échoueront
                 * proprement (résultat orcFailed) si l'EULA restait requise. */
                IUpdate_AcceptEula(upd);
                LONG idx = 0;
                IUpdateCollection_Add(to_install, upd, &idx);
                matched++;
            }
            IUpdate_Release(upd);
        }
    }

    if (matched == 0) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Aucun des patchs demandés n'a été retrouvé dans la recherche Windows Update "
                 "(inventaire périmé ?)");
        goto cleanup;
    }

    if (FAILED(IUpdateSession_CreateUpdateDownloader(session, &downloader)) || !downloader) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf), "CreateUpdateDownloader a échoué");
        goto cleanup;
    }
    IUpdateDownloader_put_Updates(downloader, to_install);
    if (FAILED(IUpdateDownloader_Download(downloader, &dl_result))) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Échec du téléchargement des %d patch(s)", matched);
        goto cleanup;
    }

    if (FAILED(IUpdateSession_CreateUpdateInstaller(session, &installer)) || !installer) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf), "CreateUpdateInstaller a échoué");
        goto cleanup;
    }
    IUpdateInstaller_put_Updates(installer, to_install);
    if (FAILED(IUpdateInstaller_Install(installer, &inst_result)) || !inst_result) {
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Échec de l'installation des %d patch(s)", matched);
        goto cleanup;
    }

    {
        OperationResultCode code = orcNotStarted;
        VARIANT_BOOL reboot_required = VARIANT_FALSE;
        IInstallationResult_get_ResultCode(inst_result, &code);
        IInstallationResult_get_RebootRequired(inst_result, &reboot_required);

        bool ok = (code == orcSucceeded || code == orcSucceededWithErrors);
        result->exit_code = ok ? 0 : -1;
        snprintf(result->stdout_buf, sizeof(result->stdout_buf),
                 "%d patch(s) traité(s), résultat=%d, redémarrage requis=%s",
                 matched, (int)code, reboot_required != VARIANT_FALSE ? "oui" : "non");

        if (ok && reboot_after && reboot_required != VARIANT_FALSE) {
            LOG_WARN("Redémarrage requis après installation des patchs — planifié dans 60s");
            _enable_shutdown_privilege();
            InitiateSystemShutdownExW(NULL, NULL, 60, FALSE, TRUE,
                SHTDN_REASON_MAJOR_OPERATINGSYSTEM | SHTDN_REASON_MINOR_UPGRADE | SHTDN_REASON_FLAG_PLANNED);
        }
    }

cleanup:
    if (inst_result)    IInstallationResult_Release(inst_result);
    if (installer)       IUpdateInstaller_Release(installer);
    if (dl_result)        IDownloadResult_Release(dl_result);
    if (downloader)       IUpdateDownloader_Release(downloader);
    if (to_install)       IUpdateCollection_Release(to_install);
    if (all_updates)      IUpdateCollection_Release(all_updates);
    if (search_result)    ISearchResult_Release(search_result);
    if (searcher)         IUpdateSearcher_Release(searcher);
    if (session)          IUpdateSession_Release(session);
    if (need_uninit)      CoUninitialize();

    return LEO_OK;
}
