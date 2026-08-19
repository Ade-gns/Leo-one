/**
 * remote_desktop.h — Bureau à distance : capture d'écran + injection
 * d'input, streaming JPEG image par image sur une connexion WSS DÉDIÉE
 * (séparée du canal de contrôle persistant, voir connection.h) ouverte à la
 * demande sur réception de LEO_MSG_REMOTE_DESKTOP_START. Voir le commentaire
 * en tête de file_transfer.c pour le précédent de ce projet : les données
 * volumineuses/haute fréquence ne passent jamais par le canal de contrôle
 * (texte JSON, 64 Ko, une seule file d'envoi) mais par une connexion
 * ouverte spécialement pour l'occasion.
 *
 * Contrats capture/input implémentés par plateforme :
 *   platform/linux/capture_linux.c   (X11 XShm/XGetImage)
 *   platform/linux/input_linux.c     (XTest)
 *   platform/windows/capture_win.c   (GDI BitBlt)          [phase suivante]
 *   platform/windows/input_win.c     (SendInput)            [phase suivante]
 *
 * Protocole binaire sur la connexion dédiée (premier octet = type — doit
 * correspondre aux constantes wireType* du relais backend, voir
 * backend/internal/infrastructure/remotedesktop/relay.go) :
 *
 *   0x01 FRAME        agent→navigateur : [type][u16 width BE][u16 height BE][u32 seq BE][JPEG...]
 *   0x10 INPUT_MOVE    navigateur→agent : [type][u16 x BE][u16 y BE]                (0..65535 normalisé)
 *   0x11 INPUT_BUTTON  navigateur→agent : [type][u8 button][u8 down]
 *   0x12 INPUT_SCROLL  navigateur→agent : [type][i16 delta BE]
 *   0x13 INPUT_KEY     navigateur→agent : [type][u8 leo_rd_key_t][u8 down]
 *   0x20 CONTROL       navigateur→agent : [type][JSON]  — seul {"type":"stop"} est traité pour l'instant
 *
 * Les messages INPUT_* sont déjà filtrés par le relais backend quand la
 * session est en mode "view" (voir Relay.pump côté Go) — l'agent ne les
 * reçoit alors normalement jamais, mais ne leur fait pas confiance
 * aveuglément pour autant : voir la revérification de control_mode dans
 * remote_desktop.c avant tout appel à l'input backend (défense en
 * profondeur, coût nul).
 */
#ifndef LEO_REMOTE_DESKTOP_H
#define LEO_REMOTE_DESKTOP_H

#include "../include/leo_agent.h"
#include "connection.h"

#include <stdint.h>

/* ─── Capture d'écran ─────────────────────────────────────────────────────── */

typedef struct leo_rd_capture leo_rd_capture_t;

/** Une frame capturée — pixels toujours en 32bpp BGRX, top-down, packés
 *  (stride == width*4, pas de padding de fin de ligne). C'est le seul format
 *  que remote_desktop.c connaît : aucune conversion de format de pixel n'est
 *  faite en dehors des modules platform/ — un XImage 24/32bpp moderne
 *  (capture_linux.c) et un DIB BI_RGB 32bpp top-down (capture_win.c, phase
 *  suivante) sont tous deux nativement BGRX-compatibles, ce qui laisse
 *  tjCompress2(TJPF_BGRX) encoder directement sans passe de conversion. */
typedef struct {
    int            width;
    int            height;
    const uint8_t *pixels; /* possédés par cap, valides jusqu'au prochain leo_rd_capture_grab()/leo_rd_capture_close() */
} leo_rd_frame_t;

/**
 * Ouvre la capture d'écran, limitée à max_width×max_height (l'implémentation
 * réduit l'image si l'écran réel est plus grand — voir le commentaire sur le
 * downscale dans capture_linux.c).
 * @return NULL si la capture est indisponible (ex: pas de display X11).
 */
leo_rd_capture_t *leo_rd_capture_open(int max_width, int max_height);

/**
 * Capture une frame. out->pixels reste valide jusqu'au prochain appel ou à
 * leo_rd_capture_close(). Peut échouer ponctuellement (ex: resize d'écran en
 * cours) sans que la session doive s'arrêter — l'appelant tolère quelques
 * échecs consécutifs avant d'abandonner (voir remote_desktop.c).
 */
bool leo_rd_capture_grab(leo_rd_capture_t *cap, leo_rd_frame_t *out);

void leo_rd_capture_close(leo_rd_capture_t *cap);

/* ─── Injection d'input ───────────────────────────────────────────────────── */

typedef struct leo_rd_input leo_rd_input_t;

/** Touches physiques, indépendantes du layout clavier — le navigateur mappe
 *  KeyboardEvent.code (déjà layout-indépendant) vers cette énumération côté
 *  TypeScript, l'agent la mappe vers un keysym X11 (input_linux.c) ou un
 *  code virtuel Win32 (input_win.c, phase suivante). Valeurs stables (contrat
 *  de wire protocol avec le frontend) : ne jamais réordonner ni réutiliser
 *  une valeur, seulement ajouter à la fin. */
typedef enum {
    LEO_KEY_UNKNOWN = 0,
    LEO_KEY_A, LEO_KEY_B, LEO_KEY_C, LEO_KEY_D, LEO_KEY_E, LEO_KEY_F, LEO_KEY_G,
    LEO_KEY_H, LEO_KEY_I, LEO_KEY_J, LEO_KEY_K, LEO_KEY_L, LEO_KEY_M, LEO_KEY_N,
    LEO_KEY_O, LEO_KEY_P, LEO_KEY_Q, LEO_KEY_R, LEO_KEY_S, LEO_KEY_T, LEO_KEY_U,
    LEO_KEY_V, LEO_KEY_W, LEO_KEY_X, LEO_KEY_Y, LEO_KEY_Z,
    LEO_KEY_0, LEO_KEY_1, LEO_KEY_2, LEO_KEY_3, LEO_KEY_4,
    LEO_KEY_5, LEO_KEY_6, LEO_KEY_7, LEO_KEY_8, LEO_KEY_9,
    LEO_KEY_ENTER, LEO_KEY_ESCAPE, LEO_KEY_BACKSPACE, LEO_KEY_TAB, LEO_KEY_SPACE,
    LEO_KEY_MINUS, LEO_KEY_EQUAL, LEO_KEY_LEFTBRACKET, LEO_KEY_RIGHTBRACKET,
    LEO_KEY_BACKSLASH, LEO_KEY_SEMICOLON, LEO_KEY_QUOTE, LEO_KEY_GRAVE,
    LEO_KEY_COMMA, LEO_KEY_PERIOD, LEO_KEY_SLASH,
    LEO_KEY_CAPSLOCK,
    LEO_KEY_F1, LEO_KEY_F2, LEO_KEY_F3, LEO_KEY_F4, LEO_KEY_F5, LEO_KEY_F6,
    LEO_KEY_F7, LEO_KEY_F8, LEO_KEY_F9, LEO_KEY_F10, LEO_KEY_F11, LEO_KEY_F12,
    LEO_KEY_PRINTSCREEN, LEO_KEY_SCROLLLOCK, LEO_KEY_PAUSE,
    LEO_KEY_INSERT, LEO_KEY_HOME, LEO_KEY_PAGEUP, LEO_KEY_DELETE,
    LEO_KEY_END, LEO_KEY_PAGEDOWN,
    LEO_KEY_RIGHT, LEO_KEY_LEFT, LEO_KEY_DOWN, LEO_KEY_UP,
    LEO_KEY_NUMLOCK,
    LEO_KEY_LCTRL, LEO_KEY_LSHIFT, LEO_KEY_LALT, LEO_KEY_LMETA,
    LEO_KEY_RCTRL, LEO_KEY_RSHIFT, LEO_KEY_RALT, LEO_KEY_RMETA,
} leo_rd_key_t;

leo_rd_input_t *leo_rd_input_open(void);

/** x/y normalisés 0..65535 sur la surface capturée (voir leo_rd_frame_t) —
 *  c'est à l'implémentation platform/ de les remettre à l'échelle de l'écran
 *  physique réel (potentiellement différent de max_width/max_height si
 *  l'image a été réduite, voir leo_rd_capture_open). */
void leo_rd_input_move(leo_rd_input_t *in, int x, int y);
void leo_rd_input_button(leo_rd_input_t *in, int button, bool down); /* button : 1=gauche 2=milieu 3=droit */
/** delta_units : un "cran" de molette par unité (signe = direction, positif
 *  = défilement vers le haut) — au frontend de normaliser deltaY en crans
 *  avant l'envoi (voir remote_desktop.h en tête de fichier, message
 *  INPUT_SCROLL). Magnitude bornée en interne par l'implémentation
 *  platform/ pour éviter qu'un delta aberrant ne déclenche une rafale
 *  d'événements. */
void leo_rd_input_scroll(leo_rd_input_t *in, int delta_units);
void leo_rd_input_key(leo_rd_input_t *in, leo_rd_key_t key, bool down);

void leo_rd_input_close(leo_rd_input_t *in);

/* ─── Session de bureau à distance ────────────────────────────────────────── */

/**
 * Démarre une session de bureau à distance en réponse à
 * LEO_MSG_REMOTE_DESKTOP_START : ouvre une connexion WSS dédiée (mTLS, même
 * certificat client que le canal de contrôle) vers ws_url, puis pompe
 * capture→JPEG→frame en boucle à ~fps images/seconde jusqu'à déconnexion,
 * message CONTROL "stop", ou leo_rd_stop()/leo_rd_stop_all().
 *
 * Non-bloquant : lance un thread détaché et retourne immédiatement. Au plus
 * UNE session active à la fois (cohérent avec la contrainte "une session par
 * agent" côté backend, voir remotedesktop.Repository.ActiveForAgent côté
 * Go) — un second appel pendant qu'une session tourne déjà échoue
 * (retourne false) sans démarrer de second thread de capture.
 *
 * @param conn        Connexion WSS de contrôle, pour LEO_MSG_REMOTE_DESKTOP_STATUS
 * @param config      Configuration de l'agent (certificat client, empreinte CA) —
 *                     référence non-owned, doit rester valide tant que la session
 *                     tourne (garanti par leo_agent_stop(), voir leo_rd_stop_all())
 * @param session_id  Identifiant de session (remote_desktop_sessions.id côté backend)
 * @param ws_url      URL wss:// de la connexion dédiée, jeton inclus en query string
 * @param mode        "view" ou "control" — "control" seul autorise l'injection d'input
 * @param fps         Images par seconde cible (déjà bornée par l'appelant)
 * @param quality     Qualité JPEG 1-100 (déjà bornée par l'appelant)
 * @param max_width   Largeur max de capture (déjà bornée par l'appelant)
 * @param max_height  Hauteur max de capture (déjà bornée par l'appelant)
 * @return true si le thread de session a démarré, false si une session est
 *         déjà active ou si le thread n'a pas pu être créé
 */
bool leo_rd_start(leo_conn_t *conn, const leo_config_t *config,
                   const char *session_id, const char *ws_url, const char *mode,
                   int fps, int quality, int max_width, int max_height);

/**
 * Demande l'arrêt de la session en cours si son session_id correspond —
 * appelé sur réception de LEO_MSG_REMOTE_DESKTOP_STOP. Ne bloque pas (signale
 * juste l'arrêt, le thread de session se termine de lui-même) ; un
 * session_id qui ne correspond à aucune session active est ignoré
 * (journalisé en warning).
 */
void leo_rd_stop(const char *session_id);

/**
 * Demande l'arrêt de toute session en cours, sans condition de session_id,
 * et ATTEND que le thread de session se termine (borné à quelques secondes)
 * — appelé depuis leo_agent_stop() pour ne jamais libérer ag->config/ag->conn
 * (dont le thread de session tient une référence non-owning) pendant qu'il
 * tourne encore.
 * @return true si aucune session n'était active ou si elle s'est terminée
 *         dans le délai ; false si le thread de session n'a pas rejoint à
 *         temps — dans ce cas l'appelant NE DOIT PAS libérer de mémoire
 *         référencée par la session (même contrat que leo_conn_destroy()).
 */
bool leo_rd_stop_all(void);

#endif /* LEO_REMOTE_DESKTOP_H */
