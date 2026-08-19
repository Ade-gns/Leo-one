/**
 * capture_linux.c — Capture d'écran X11 pour le bureau à distance (voir
 * ../../src/remote_desktop.h). XShm (mémoire partagée, évite une copie
 * réseau/IPC à chaque frame) si le serveur X la supporte, repli XGetImage
 * sinon (ex: affichage distant/forwardé, ou XShm désactivé côté serveur).
 *
 * Limite connue et volontaire (MVP) : X11 uniquement (pas de Wayland — la
 * capture d'écran y nécessite le portail XDG + PipeWire, hors scope de ce
 * premier passage), et seulement le visual TrueColor 24/32bpp LSBFirst
 * standard (validé à l'ouverture — voir _visual_supported ci-dessous) : un
 * serveur X exotique (byte order big-endian, palette indexée...) fait
 * échouer leo_rd_capture_open() proprement plutôt que produire une image aux
 * couleurs corrompues.
 */
#include "../../src/remote_desktop.h"
#include "../../src/logger.h"

#include <X11/Xlib.h>
#include <X11/Xutil.h>       /* XDestroyImage() */
#include <X11/extensions/XShm.h>

#include <sys/ipc.h>
#include <sys/shm.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

/* Xlib n'est pas thread-safe par défaut. XInitThreads() doit être appelée
 * UNE SEULE FOIS, avant toute autre fonction Xlib, quel que soit le nombre
 * de Display* ouverts ensuite (capture_linux.c et input_linux.c ouvrent
 * chacun leur propre connexion — voir le commentaire de input_linux.c sur
 * pourquoi elles restent séparées). pthread_once garantit cette unicité même
 * si les deux modules s'initialisent concurremment. */
static pthread_once_t _xinit_once = PTHREAD_ONCE_INIT;
static void _xinit(void) { XInitThreads(); }

struct leo_rd_capture {
    Display *dpy;
    Window   root;
    int      screen;
    int      screen_w, screen_h; /* résolution physique réelle de l'écran X */
    int      out_w, out_h;       /* résolution restituée (<= max_width/max_height, ratio préservé) */

    bool             use_shm;
    XShmSegmentInfo  shm_info;
    XImage          *ximage;     /* réutilisée à chaque frame (XShm), ou NULL hors capture (XGetImage) */

    uint8_t *out_buf;            /* toujours BGRX packé (stride == out_w*4) — voir leo_rd_frame_t */
};

/** Valide que le visual par défaut correspond au layout BGRX 32bpp/LSBFirst
 *  packé attendu par le reste du pipeline (voir le commentaire de
 *  leo_rd_frame_t dans remote_desktop.h) — plutôt que de produire une image
 *  aux couleurs silencieusement fausses sur un serveur X inhabituel. */
static bool _visual_supported(Display *dpy, int screen) {
    if (ImageByteOrder(dpy) != LSBFirst) return false;
    int depth = DefaultDepth(dpy, screen);
    if (depth != 24 && depth != 32) return false;

    Visual *vis = DefaultVisual(dpy, screen);
    return vis->red_mask == 0x00FF0000 &&
           vis->green_mask == 0x0000FF00 &&
           vis->blue_mask == 0x000000FF;
}

static void _compute_output_size(int screen_w, int screen_h, int max_w, int max_h,
                                  int *out_w, int *out_h) {
    if (screen_w <= max_w && screen_h <= max_h) {
        *out_w = screen_w;
        *out_h = screen_h;
        return;
    }
    double scale = 1.0;
    if (screen_w > max_w) scale = (double)max_w / (double)screen_w;
    if (screen_h > max_h) {
        double scale_h = (double)max_h / (double)screen_h;
        if (scale_h < scale) scale = scale_h;
    }
    *out_w = (int)(screen_w * scale);
    *out_h = (int)(screen_h * scale);
    if (*out_w < 1) *out_w = 1;
    if (*out_h < 1) *out_h = 1;
}

static bool _try_setup_shm(leo_rd_capture_t *cap) {
    if (!XShmQueryExtension(cap->dpy)) return false;

    cap->ximage = XShmCreateImage(cap->dpy, DefaultVisual(cap->dpy, cap->screen),
                                   (unsigned int)DefaultDepth(cap->dpy, cap->screen),
                                   ZPixmap, NULL, &cap->shm_info,
                                   (unsigned int)cap->screen_w, (unsigned int)cap->screen_h);
    if (!cap->ximage) return false;

    cap->shm_info.shmid = shmget(IPC_PRIVATE,
                                  (size_t)cap->ximage->bytes_per_line * (size_t)cap->ximage->height,
                                  IPC_CREAT | 0600);
    if (cap->shm_info.shmid < 0) {
        XDestroyImage(cap->ximage);
        cap->ximage = NULL;
        return false;
    }

    cap->shm_info.shmaddr = cap->ximage->data = shmat(cap->shm_info.shmid, NULL, 0);
    cap->shm_info.readOnly = False;
    if (cap->shm_info.shmaddr == (char *)-1) {
        shmctl(cap->shm_info.shmid, IPC_RMID, NULL);
        XDestroyImage(cap->ximage);
        cap->ximage = NULL;
        return false;
    }

    if (!XShmAttach(cap->dpy, &cap->shm_info)) {
        shmdt(cap->shm_info.shmaddr);
        shmctl(cap->shm_info.shmid, IPC_RMID, NULL);
        XDestroyImage(cap->ximage);
        cap->ximage = NULL;
        return false;
    }
    XSync(cap->dpy, False);

    /* Le segment est marqué pour suppression dès maintenant : le noyau ne le
     * libère réellement qu'une fois le dernier attachement détaché (le
     * nôtre, dans leo_rd_capture_close), donc rien ne fuit si l'agent
     * crashe avant d'y arriver — évite un segment shm orphelin. */
    shmctl(cap->shm_info.shmid, IPC_RMID, NULL);

    return true;
}

leo_rd_capture_t *leo_rd_capture_open(int max_width, int max_height) {
    pthread_once(&_xinit_once, _xinit);

    leo_rd_capture_t *cap = calloc(1, sizeof(*cap));
    if (!cap) return NULL;

    cap->dpy = XOpenDisplay(NULL);
    if (!cap->dpy) {
        LOG_ERROR("Bureau à distance : impossible d'ouvrir le display X11 (DISPLAY=%s)",
                  getenv("DISPLAY") ? getenv("DISPLAY") : "(non défini)");
        free(cap);
        return NULL;
    }

    cap->screen = DefaultScreen(cap->dpy);
    cap->root   = RootWindow(cap->dpy, cap->screen);

    if (!_visual_supported(cap->dpy, cap->screen)) {
        LOG_ERROR("Bureau à distance : visual X11 non supporté (attendu TrueColor 24/32bpp LSBFirst)");
        XCloseDisplay(cap->dpy);
        free(cap);
        return NULL;
    }

    cap->screen_w = DisplayWidth(cap->dpy, cap->screen);
    cap->screen_h = DisplayHeight(cap->dpy, cap->screen);
    _compute_output_size(cap->screen_w, cap->screen_h, max_width, max_height, &cap->out_w, &cap->out_h);

    cap->out_buf = malloc((size_t)cap->out_w * (size_t)cap->out_h * 4);
    if (!cap->out_buf) {
        XCloseDisplay(cap->dpy);
        free(cap);
        return NULL;
    }

    cap->use_shm = _try_setup_shm(cap);
    if (!cap->use_shm) {
        LOG_WARN("Bureau à distance : XShm indisponible, repli sur XGetImage (plus coûteux en CPU)");
    }

    LOG_INFO("Bureau à distance : capture X11 ouverte (%dx%d -> %dx%d, shm=%s)",
             cap->screen_w, cap->screen_h, cap->out_w, cap->out_h, cap->use_shm ? "oui" : "non");
    return cap;
}

/** Recopie/réduit `src` (bytes_per_line `stride`, screen_w×screen_h) dans
 *  cap->out_buf (packé, out_w×out_h) — même chemin que la capture soit
 *  identique en résolution (simple recopie ligne à ligne, pour retirer le
 *  padding éventuel de bytes_per_line) ou réduite (plus proche voisin :
 *  largement suffisant pour un flux de bureau à distance à qualité JPEG
 *  modérée, pas besoin d'un filtre de zone/bilinéaire). */
static void _repack_and_scale(leo_rd_capture_t *cap, const uint8_t *src, int stride) {
    if (cap->out_w == cap->screen_w && cap->out_h == cap->screen_h) {
        for (int y = 0; y < cap->screen_h; y++) {
            memcpy(cap->out_buf + (size_t)y * cap->out_w * 4,
                   src + (size_t)y * stride,
                   (size_t)cap->out_w * 4);
        }
        return;
    }

    for (int y = 0; y < cap->out_h; y++) {
        int sy = (int)((int64_t)y * cap->screen_h / cap->out_h);
        const uint8_t *src_row = src + (size_t)sy * stride;
        uint8_t *dst_row = cap->out_buf + (size_t)y * cap->out_w * 4;
        for (int x = 0; x < cap->out_w; x++) {
            int sx = (int)((int64_t)x * cap->screen_w / cap->out_w);
            memcpy(dst_row + (size_t)x * 4, src_row + (size_t)sx * 4, 4);
        }
    }
}

bool leo_rd_capture_grab(leo_rd_capture_t *cap, leo_rd_frame_t *out) {
    if (!cap || !out) return false;

    if (cap->use_shm) {
        if (!XShmGetImage(cap->dpy, cap->root, cap->ximage, 0, 0, AllPlanes)) return false;
        _repack_and_scale(cap, (const uint8_t *)cap->ximage->data, cap->ximage->bytes_per_line);
    } else {
        XImage *img = XGetImage(cap->dpy, cap->root, 0, 0,
                                 (unsigned int)cap->screen_w, (unsigned int)cap->screen_h,
                                 AllPlanes, ZPixmap);
        if (!img) return false;
        _repack_and_scale(cap, (const uint8_t *)img->data, img->bytes_per_line);
        XDestroyImage(img);
    }

    out->width  = cap->out_w;
    out->height = cap->out_h;
    out->pixels = cap->out_buf;
    return true;
}

void leo_rd_capture_close(leo_rd_capture_t *cap) {
    if (!cap) return;

    if (cap->use_shm) {
        XShmDetach(cap->dpy, &cap->shm_info);
        XDestroyImage(cap->ximage);
        shmdt(cap->shm_info.shmaddr);
    }
    free(cap->out_buf);
    XCloseDisplay(cap->dpy);
    free(cap);
}
