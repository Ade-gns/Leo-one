/**
 * input_win.c — Injection d'input via SendInput() (Win32), pour le bureau à
 * distance (voir ../../src/remote_desktop.h).
 *
 * Coordonnées souris : SendInput avec MOUSEEVENTF_ABSOLUTE |
 * MOUSEEVENTF_VIRTUALDESK attend déjà des dx/dy normalisés 0..65535 sur
 * l'écran virtuel COMPLET — contrairement à input_linux.c (XTest, qui
 * attend des pixels réels et doit donc remettre à l'échelle lui-même), rien
 * à recalculer ici : le contrat du protocole (0..65535 normalisé, voir
 * remote_desktop.h) correspond directement à l'API Win32.
 *
 * Vérifié uniquement à la compilation/l'édition de liens (cross-compilation
 * mingw) — même limite connue que capture_win.c.
 */
#include "../../src/remote_desktop.h"
#include "../../src/logger.h"

#include <winsock2.h>
#include <windows.h>

#include <stdlib.h>
#include <string.h>

struct leo_rd_input {
    int virt_w, virt_h; /* non utilisé pour le déplacement (VIRTUALDESK s'en charge),
                          * conservé pour un diagnostic éventuel */
};

/** Mappe une touche physique (leo_rd_key_t, voir remote_desktop.h) vers un
 *  code virtuel Win32 (VK_*) — 0 (non défini) pour toute valeur non
 *  reconnue, y compris LEO_KEY_UNKNOWN : l'appelant ignore la touche
 *  plutôt que d'injecter n'importe quoi. */
static WORD _vk_for_key(leo_rd_key_t key) {
    switch (key) {
    case LEO_KEY_A: return 'A'; case LEO_KEY_B: return 'B';
    case LEO_KEY_C: return 'C'; case LEO_KEY_D: return 'D';
    case LEO_KEY_E: return 'E'; case LEO_KEY_F: return 'F';
    case LEO_KEY_G: return 'G'; case LEO_KEY_H: return 'H';
    case LEO_KEY_I: return 'I'; case LEO_KEY_J: return 'J';
    case LEO_KEY_K: return 'K'; case LEO_KEY_L: return 'L';
    case LEO_KEY_M: return 'M'; case LEO_KEY_N: return 'N';
    case LEO_KEY_O: return 'O'; case LEO_KEY_P: return 'P';
    case LEO_KEY_Q: return 'Q'; case LEO_KEY_R: return 'R';
    case LEO_KEY_S: return 'S'; case LEO_KEY_T: return 'T';
    case LEO_KEY_U: return 'U'; case LEO_KEY_V: return 'V';
    case LEO_KEY_W: return 'W'; case LEO_KEY_X: return 'X';
    case LEO_KEY_Y: return 'Y'; case LEO_KEY_Z: return 'Z';

    case LEO_KEY_0: return '0'; case LEO_KEY_1: return '1';
    case LEO_KEY_2: return '2'; case LEO_KEY_3: return '3';
    case LEO_KEY_4: return '4'; case LEO_KEY_5: return '5';
    case LEO_KEY_6: return '6'; case LEO_KEY_7: return '7';
    case LEO_KEY_8: return '8'; case LEO_KEY_9: return '9';

    case LEO_KEY_ENTER:     return VK_RETURN;
    case LEO_KEY_ESCAPE:    return VK_ESCAPE;
    case LEO_KEY_BACKSPACE: return VK_BACK;
    case LEO_KEY_TAB:       return VK_TAB;
    case LEO_KEY_SPACE:     return VK_SPACE;

    case LEO_KEY_MINUS:        return VK_OEM_MINUS;
    case LEO_KEY_EQUAL:        return VK_OEM_PLUS;
    case LEO_KEY_LEFTBRACKET:  return VK_OEM_4;
    case LEO_KEY_RIGHTBRACKET: return VK_OEM_6;
    case LEO_KEY_BACKSLASH:    return VK_OEM_5;
    case LEO_KEY_SEMICOLON:    return VK_OEM_1;
    case LEO_KEY_QUOTE:        return VK_OEM_7;
    case LEO_KEY_GRAVE:        return VK_OEM_3;
    case LEO_KEY_COMMA:        return VK_OEM_COMMA;
    case LEO_KEY_PERIOD:       return VK_OEM_PERIOD;
    case LEO_KEY_SLASH:        return VK_OEM_2;

    case LEO_KEY_CAPSLOCK: return VK_CAPITAL;

    case LEO_KEY_F1:  return VK_F1;  case LEO_KEY_F2:  return VK_F2;
    case LEO_KEY_F3:  return VK_F3;  case LEO_KEY_F4:  return VK_F4;
    case LEO_KEY_F5:  return VK_F5;  case LEO_KEY_F6:  return VK_F6;
    case LEO_KEY_F7:  return VK_F7;  case LEO_KEY_F8:  return VK_F8;
    case LEO_KEY_F9:  return VK_F9;  case LEO_KEY_F10: return VK_F10;
    case LEO_KEY_F11: return VK_F11; case LEO_KEY_F12: return VK_F12;

    case LEO_KEY_PRINTSCREEN: return VK_SNAPSHOT;
    case LEO_KEY_SCROLLLOCK:  return VK_SCROLL;
    case LEO_KEY_PAUSE:       return VK_PAUSE;
    case LEO_KEY_INSERT:      return VK_INSERT;
    case LEO_KEY_HOME:        return VK_HOME;
    case LEO_KEY_PAGEUP:      return VK_PRIOR;
    case LEO_KEY_DELETE:      return VK_DELETE;
    case LEO_KEY_END:         return VK_END;
    case LEO_KEY_PAGEDOWN:    return VK_NEXT;

    case LEO_KEY_RIGHT: return VK_RIGHT;
    case LEO_KEY_LEFT:  return VK_LEFT;
    case LEO_KEY_DOWN:  return VK_DOWN;
    case LEO_KEY_UP:    return VK_UP;

    case LEO_KEY_NUMLOCK: return VK_NUMLOCK;

    case LEO_KEY_LCTRL:  return VK_LCONTROL; case LEO_KEY_RCTRL:  return VK_RCONTROL;
    case LEO_KEY_LSHIFT: return VK_LSHIFT;   case LEO_KEY_RSHIFT: return VK_RSHIFT;
    case LEO_KEY_LALT:   return VK_LMENU;    case LEO_KEY_RALT:   return VK_RMENU;
    case LEO_KEY_LMETA:  return VK_LWIN;     case LEO_KEY_RMETA:  return VK_RWIN;

    case LEO_KEY_UNKNOWN:
    default:
        return 0;
    }
}

leo_rd_input_t *leo_rd_input_open(void) {
    leo_rd_input_t *in = calloc(1, sizeof(*in));
    if (!in) return NULL;

    in->virt_w = GetSystemMetrics(SM_CXVIRTUALSCREEN);
    in->virt_h = GetSystemMetrics(SM_CYVIRTUALSCREEN);
    if (in->virt_w <= 0 || in->virt_h <= 0) {
        LOG_ERROR("Bureau à distance : résolution d'écran virtuel invalide (input)");
        free(in);
        return NULL;
    }
    return in;
}

void leo_rd_input_move(leo_rd_input_t *in, int x, int y) {
    if (!in) return;
    INPUT input;
    memset(&input, 0, sizeof(input));
    input.type      = INPUT_MOUSE;
    input.mi.dx      = x;
    input.mi.dy      = y;
    input.mi.dwFlags = MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_VIRTUALDESK;
    SendInput(1, &input, sizeof(INPUT));
}

void leo_rd_input_button(leo_rd_input_t *in, int button, bool down) {
    if (!in) return;
    INPUT input;
    memset(&input, 0, sizeof(input));
    input.type = INPUT_MOUSE;
    switch (button) {
    case 1: input.mi.dwFlags = down ? MOUSEEVENTF_LEFTDOWN   : MOUSEEVENTF_LEFTUP;   break;
    case 2: input.mi.dwFlags = down ? MOUSEEVENTF_MIDDLEDOWN : MOUSEEVENTF_MIDDLEUP; break;
    case 3: input.mi.dwFlags = down ? MOUSEEVENTF_RIGHTDOWN  : MOUSEEVENTF_RIGHTUP;  break;
    default: return;
    }
    SendInput(1, &input, sizeof(INPUT));
}

/* Un "cran" (voir remote_desktop.h) == WHEEL_DELTA (120), unité native de
 * la molette Win32 — pas de conversion à faire, contrairement à
 * input_linux.c (XTest, qui simule des clics bouton 4/5 par cran). */
void leo_rd_input_scroll(leo_rd_input_t *in, int delta_units) {
    if (!in || delta_units == 0) return;
    INPUT input;
    memset(&input, 0, sizeof(input));
    input.type          = INPUT_MOUSE;
    input.mi.dwFlags     = MOUSEEVENTF_WHEEL;
    input.mi.mouseData   = (DWORD)(delta_units * WHEEL_DELTA);
    SendInput(1, &input, sizeof(INPUT));
}

void leo_rd_input_key(leo_rd_input_t *in, leo_rd_key_t key, bool down) {
    if (!in) return;
    WORD vk = _vk_for_key(key);
    if (vk == 0) return;

    INPUT input;
    memset(&input, 0, sizeof(input));
    input.type       = INPUT_KEYBOARD;
    input.ki.wVk      = vk;
    input.ki.dwFlags  = down ? 0 : KEYEVENTF_KEYUP;
    SendInput(1, &input, sizeof(INPUT));
}

void leo_rd_input_close(leo_rd_input_t *in) {
    free(in);
}
