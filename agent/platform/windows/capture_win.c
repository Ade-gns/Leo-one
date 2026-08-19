/**
 * capture_win.c — Capture d'écran GDI pour le bureau à distance (voir
 * ../../src/remote_desktop.h). BitBlt depuis un DC écran vers un DIB 32bpp
 * top-down, dont le contenu mémoire (BGRX, little-endian) correspond
 * directement au contrat BGRX de leo_rd_frame_t — aucune conversion de
 * format de pixel nécessaire, comme sur Linux (voir capture_linux.c).
 *
 * Écran VIRTUEL entier (SM_CXVIRTUALSCREEN & co., pas SM_CXSCREEN) : couvre
 * correctement les configurations multi-écrans, y compris quand l'écran
 * principal n'est pas à l'origine (0,0) — un moniteur secondaire placé à
 * gauche/au-dessus produit une origine négative (SM_XVIRTUALSCREEN/
 * SM_YVIRTUALSCREEN), ignorée à tort si on utilisait juste GetSystemMetrics
 * (SM_CXSCREEN/SM_CYSCREEN), qui ne décrit que l'écran principal.
 *
 * Vérifié uniquement à la compilation/l'édition de liens (cross-compilation
 * mingw) — pas de machine Windows dans ce sandbox pour un test d'exécution,
 * même limite connue que le reste du portage Windows de l'agent.
 */
#include "../../src/remote_desktop.h"
#include "../../src/logger.h"

#include <winsock2.h>
#include <windows.h>

#include <stdlib.h>
#include <string.h>

struct leo_rd_capture {
    HDC      screen_dc;
    HDC      mem_dc;
    HBITMAP  bitmap;
    HBITMAP  old_bitmap;
    void    *dib_bits;   /* pixels du DIB, BGRX 32bpp top-down packé */

    int virt_x, virt_y;  /* origine de l'écran virtuel (peut être négative) */
    int virt_w, virt_h;  /* résolution physique de l'écran virtuel complet */
    int out_w, out_h;    /* résolution restituée (<= max_width/max_height, ratio préservé) */

    uint8_t *out_buf;
};

leo_rd_capture_t *leo_rd_capture_open(int max_width, int max_height) {
    leo_rd_capture_t *cap = calloc(1, sizeof(*cap));
    if (!cap) return NULL;

    cap->virt_x = GetSystemMetrics(SM_XVIRTUALSCREEN);
    cap->virt_y = GetSystemMetrics(SM_YVIRTUALSCREEN);
    cap->virt_w = GetSystemMetrics(SM_CXVIRTUALSCREEN);
    cap->virt_h = GetSystemMetrics(SM_CYVIRTUALSCREEN);
    if (cap->virt_w <= 0 || cap->virt_h <= 0) {
        LOG_ERROR("Bureau à distance : résolution d'écran virtuel invalide");
        free(cap);
        return NULL;
    }

    cap->screen_dc = GetDC(NULL);
    if (!cap->screen_dc) {
        LOG_ERROR("Bureau à distance : GetDC(NULL) a échoué");
        free(cap);
        return NULL;
    }

    cap->mem_dc = CreateCompatibleDC(cap->screen_dc);
    if (!cap->mem_dc) {
        LOG_ERROR("Bureau à distance : CreateCompatibleDC a échoué");
        ReleaseDC(NULL, cap->screen_dc);
        free(cap);
        return NULL;
    }

    BITMAPINFO bmi;
    memset(&bmi, 0, sizeof(bmi));
    bmi.bmiHeader.biSize        = sizeof(BITMAPINFOHEADER);
    bmi.bmiHeader.biWidth       = cap->virt_w;
    bmi.bmiHeader.biHeight      = -cap->virt_h; /* négatif = top-down, requis par le contrat leo_rd_frame_t */
    bmi.bmiHeader.biPlanes      = 1;
    bmi.bmiHeader.biBitCount    = 32;
    bmi.bmiHeader.biCompression = BI_RGB;

    cap->bitmap = CreateDIBSection(cap->mem_dc, &bmi, DIB_RGB_COLORS, &cap->dib_bits, NULL, 0);
    if (!cap->bitmap || !cap->dib_bits) {
        LOG_ERROR("Bureau à distance : CreateDIBSection a échoué");
        DeleteDC(cap->mem_dc);
        ReleaseDC(NULL, cap->screen_dc);
        free(cap);
        return NULL;
    }
    cap->old_bitmap = (HBITMAP)SelectObject(cap->mem_dc, cap->bitmap);

    leo_rd_compute_output_size(cap->virt_w, cap->virt_h, max_width, max_height, &cap->out_w, &cap->out_h);
    cap->out_buf = malloc((size_t)cap->out_w * (size_t)cap->out_h * 4);
    if (!cap->out_buf) {
        SelectObject(cap->mem_dc, cap->old_bitmap);
        DeleteObject(cap->bitmap);
        DeleteDC(cap->mem_dc);
        ReleaseDC(NULL, cap->screen_dc);
        free(cap);
        return NULL;
    }

    LOG_INFO("Bureau à distance : capture GDI ouverte (%dx%d -> %dx%d, origine %d,%d)",
             cap->virt_w, cap->virt_h, cap->out_w, cap->out_h, cap->virt_x, cap->virt_y);
    return cap;
}

bool leo_rd_capture_grab(leo_rd_capture_t *cap, leo_rd_frame_t *out) {
    if (!cap || !out) return false;

    /* CAPTUREBLT (combiné à SRCCOPY, pas un ROP à part entière) : inclut
     * aussi les fenêtres en couche (WS_EX_LAYERED, ex: overlays
     * semi-transparents) dans la capture — sans ce flag, BitBlt les
     * ignorerait silencieusement. */
    if (!BitBlt(cap->mem_dc, 0, 0, cap->virt_w, cap->virt_h,
                cap->screen_dc, cap->virt_x, cap->virt_y, SRCCOPY | CAPTUREBLT)) {
        return false;
    }

    /* DIB 32bpp : toujours packé (biWidth*4 est déjà un multiple de 4, pas
     * d'alignement de fin de ligne à retirer) — stride == virt_w*4 exact. */
    int stride = cap->virt_w * 4;
    leo_rd_repack_scale(cap->out_buf, cap->out_w, cap->out_h,
                         (const uint8_t *)cap->dib_bits, cap->virt_w, cap->virt_h, stride);

    out->width  = cap->out_w;
    out->height = cap->out_h;
    out->pixels = cap->out_buf;
    return true;
}

void leo_rd_capture_close(leo_rd_capture_t *cap) {
    if (!cap) return;
    SelectObject(cap->mem_dc, cap->old_bitmap);
    DeleteObject(cap->bitmap);
    DeleteDC(cap->mem_dc);
    ReleaseDC(NULL, cap->screen_dc);
    free(cap->out_buf);
    free(cap);
}
