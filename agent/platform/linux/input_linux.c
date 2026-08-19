/**
 * input_linux.c — Injection d'input via l'extension X11 XTest, pour le
 * bureau à distance (voir ../../src/remote_desktop.h).
 *
 * Connexion X11 dédiée, séparée de celle de capture_linux.c : chaque
 * Display* n'est utilisé que depuis les fonctions leo_rd_input_xxx /
 * leo_rd_capture_xxx appelées séquentiellement par le même thread de session
 * (voir remote_desktop.c), donc rien n'impose deux connexions distinctes
 * pour la sûreté — mais les garder séparées évite tout couplage accidentel
 * entre les deux modules platform/ (chacun peut s'ouvrir/fermer
 * indépendamment, ex: mode "view" qui n'ouvre jamais ce module du tout).
 */
#include "../../src/remote_desktop.h"
#include "../../src/logger.h"

#include <X11/Xlib.h>
#include <X11/keysym.h>
#include <X11/extensions/XTest.h>

#include <pthread.h>
#include <stdlib.h>

/* Voir capture_linux.c pour le raisonnement complet — XInitThreads() doit
 * être appelée une seule fois avant toute fonction Xlib, pthread_once
 * garantit l'unicité même si capture/input s'initialisent concurremment. */
static pthread_once_t _xinit_once = PTHREAD_ONCE_INIT;
static void _xinit(void) { XInitThreads(); }

struct leo_rd_input {
    Display *dpy;
    int      screen;
    int      screen_w, screen_h;
};

/** Mappe une touche physique (leo_rd_key_t, voir remote_desktop.h) vers un
 *  keysym X11 — XKeysymToKeycode() fait ensuite la conversion finale vers le
 *  keycode réellement injecté par XTestFakeKeyEvent. NoSymbol pour toute
 *  valeur non reconnue (y compris LEO_KEY_UNKNOWN) : l'appelant ignore la
 *  touche plutôt que d'injecter n'importe quoi. */
static KeySym _keysym_for(leo_rd_key_t key) {
    switch (key) {
    case LEO_KEY_A: return XK_a; case LEO_KEY_B: return XK_b;
    case LEO_KEY_C: return XK_c; case LEO_KEY_D: return XK_d;
    case LEO_KEY_E: return XK_e; case LEO_KEY_F: return XK_f;
    case LEO_KEY_G: return XK_g; case LEO_KEY_H: return XK_h;
    case LEO_KEY_I: return XK_i; case LEO_KEY_J: return XK_j;
    case LEO_KEY_K: return XK_k; case LEO_KEY_L: return XK_l;
    case LEO_KEY_M: return XK_m; case LEO_KEY_N: return XK_n;
    case LEO_KEY_O: return XK_o; case LEO_KEY_P: return XK_p;
    case LEO_KEY_Q: return XK_q; case LEO_KEY_R: return XK_r;
    case LEO_KEY_S: return XK_s; case LEO_KEY_T: return XK_t;
    case LEO_KEY_U: return XK_u; case LEO_KEY_V: return XK_v;
    case LEO_KEY_W: return XK_w; case LEO_KEY_X: return XK_x;
    case LEO_KEY_Y: return XK_y; case LEO_KEY_Z: return XK_z;

    case LEO_KEY_0: return XK_0; case LEO_KEY_1: return XK_1;
    case LEO_KEY_2: return XK_2; case LEO_KEY_3: return XK_3;
    case LEO_KEY_4: return XK_4; case LEO_KEY_5: return XK_5;
    case LEO_KEY_6: return XK_6; case LEO_KEY_7: return XK_7;
    case LEO_KEY_8: return XK_8; case LEO_KEY_9: return XK_9;

    case LEO_KEY_ENTER:     return XK_Return;
    case LEO_KEY_ESCAPE:    return XK_Escape;
    case LEO_KEY_BACKSPACE: return XK_BackSpace;
    case LEO_KEY_TAB:       return XK_Tab;
    case LEO_KEY_SPACE:     return XK_space;

    case LEO_KEY_MINUS:        return XK_minus;
    case LEO_KEY_EQUAL:        return XK_equal;
    case LEO_KEY_LEFTBRACKET:  return XK_bracketleft;
    case LEO_KEY_RIGHTBRACKET: return XK_bracketright;
    case LEO_KEY_BACKSLASH:    return XK_backslash;
    case LEO_KEY_SEMICOLON:    return XK_semicolon;
    case LEO_KEY_QUOTE:        return XK_apostrophe;
    case LEO_KEY_GRAVE:        return XK_grave;
    case LEO_KEY_COMMA:        return XK_comma;
    case LEO_KEY_PERIOD:       return XK_period;
    case LEO_KEY_SLASH:        return XK_slash;

    case LEO_KEY_CAPSLOCK: return XK_Caps_Lock;

    case LEO_KEY_F1:  return XK_F1;  case LEO_KEY_F2:  return XK_F2;
    case LEO_KEY_F3:  return XK_F3;  case LEO_KEY_F4:  return XK_F4;
    case LEO_KEY_F5:  return XK_F5;  case LEO_KEY_F6:  return XK_F6;
    case LEO_KEY_F7:  return XK_F7;  case LEO_KEY_F8:  return XK_F8;
    case LEO_KEY_F9:  return XK_F9;  case LEO_KEY_F10: return XK_F10;
    case LEO_KEY_F11: return XK_F11; case LEO_KEY_F12: return XK_F12;

    case LEO_KEY_PRINTSCREEN: return XK_Print;
    case LEO_KEY_SCROLLLOCK:  return XK_Scroll_Lock;
    case LEO_KEY_PAUSE:       return XK_Pause;
    case LEO_KEY_INSERT:      return XK_Insert;
    case LEO_KEY_HOME:        return XK_Home;
    case LEO_KEY_PAGEUP:      return XK_Page_Up;
    case LEO_KEY_DELETE:      return XK_Delete;
    case LEO_KEY_END:         return XK_End;
    case LEO_KEY_PAGEDOWN:    return XK_Page_Down;

    case LEO_KEY_RIGHT: return XK_Right;
    case LEO_KEY_LEFT:  return XK_Left;
    case LEO_KEY_DOWN:  return XK_Down;
    case LEO_KEY_UP:    return XK_Up;

    case LEO_KEY_NUMLOCK: return XK_Num_Lock;

    case LEO_KEY_LCTRL:  return XK_Control_L; case LEO_KEY_RCTRL:  return XK_Control_R;
    case LEO_KEY_LSHIFT: return XK_Shift_L;   case LEO_KEY_RSHIFT: return XK_Shift_R;
    case LEO_KEY_LALT:   return XK_Alt_L;     case LEO_KEY_RALT:   return XK_Alt_R;
    case LEO_KEY_LMETA:  return XK_Super_L;   case LEO_KEY_RMETA:  return XK_Super_R;

    case LEO_KEY_UNKNOWN:
    default:
        return NoSymbol;
    }
}

leo_rd_input_t *leo_rd_input_open(void) {
    pthread_once(&_xinit_once, _xinit);

    leo_rd_input_t *in = calloc(1, sizeof(*in));
    if (!in) return NULL;

    in->dpy = XOpenDisplay(NULL);
    if (!in->dpy) {
        LOG_ERROR("Bureau à distance : impossible d'ouvrir le display X11 pour l'input");
        free(in);
        return NULL;
    }

    int ev, err, major, minor;
    if (!XTestQueryExtension(in->dpy, &ev, &err, &major, &minor)) {
        LOG_ERROR("Bureau à distance : extension XTest indisponible sur ce serveur X11");
        XCloseDisplay(in->dpy);
        free(in);
        return NULL;
    }

    in->screen   = DefaultScreen(in->dpy);
    in->screen_w = DisplayWidth(in->dpy, in->screen);
    in->screen_h = DisplayHeight(in->dpy, in->screen);
    return in;
}

void leo_rd_input_move(leo_rd_input_t *in, int x, int y) {
    if (!in) return;
    int px = (int)((int64_t)x * (in->screen_w - 1) / 65535);
    int py = (int)((int64_t)y * (in->screen_h - 1) / 65535);
    XTestFakeMotionEvent(in->dpy, in->screen, px, py, CurrentTime);
    XFlush(in->dpy);
}

void leo_rd_input_button(leo_rd_input_t *in, int button, bool down) {
    if (!in || button < 1 || button > 3) return;
    XTestFakeButtonEvent(in->dpy, (unsigned int)button, down, CurrentTime);
    XFlush(in->dpy);
}

/* Pas de défilement fluide via XTest : chaque "cran" est simulé par un clic
 * bouton 4 (haut) ou 5 (bas), convention X11 historique toujours en vigueur.
 * Bornage à 10 crans par appel : un delta aberrant (bug/abus côté client) ne
 * doit pas déclencher une rafale de dizaines de clics synthétiques. */
void leo_rd_input_scroll(leo_rd_input_t *in, int delta_units) {
    if (!in || delta_units == 0) return;
    unsigned int btn = delta_units > 0 ? 4 : 5;
    int count = delta_units > 0 ? delta_units : -delta_units;
    if (count > 10) count = 10;
    for (int i = 0; i < count; i++) {
        XTestFakeButtonEvent(in->dpy, btn, True, CurrentTime);
        XTestFakeButtonEvent(in->dpy, btn, False, CurrentTime);
    }
    XFlush(in->dpy);
}

void leo_rd_input_key(leo_rd_input_t *in, leo_rd_key_t key, bool down) {
    if (!in) return;
    KeySym ks = _keysym_for(key);
    if (ks == NoSymbol) return;
    KeyCode kc = XKeysymToKeycode(in->dpy, ks);
    if (kc == 0) return;
    XTestFakeKeyEvent(in->dpy, kc, down, CurrentTime);
    XFlush(in->dpy);
}

void leo_rd_input_close(leo_rd_input_t *in) {
    if (!in) return;
    XCloseDisplay(in->dpy);
    free(in);
}
