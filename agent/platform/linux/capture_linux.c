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
    leo_rd_compute_output_size(cap->screen_w, cap->screen_h, max_width, max_height, &cap->out_w, &cap->out_h);

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

bool leo_rd_capture_grab(leo_rd_capture_t *cap, leo_rd_frame_t *out) {
    if (!cap || !out) return false;

    if (cap->use_shm) {
        if (!XShmGetImage(cap->dpy, cap->root, cap->ximage, 0, 0, AllPlanes)) return false;
        leo_rd_repack_scale(cap->out_buf, cap->out_w, cap->out_h,
                             (const uint8_t *)cap->ximage->data, cap->screen_w, cap->screen_h,
                             cap->ximage->bytes_per_line);
    } else {
        XImage *img = XGetImage(cap->dpy, cap->root, 0, 0,
                                 (unsigned int)cap->screen_w, (unsigned int)cap->screen_h,
                                 AllPlanes, ZPixmap);
        if (!img) return false;
        leo_rd_repack_scale(cap->out_buf, cap->out_w, cap->out_h,
                             (const uint8_t *)img->data, cap->screen_w, cap->screen_h,
                             img->bytes_per_line);
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
