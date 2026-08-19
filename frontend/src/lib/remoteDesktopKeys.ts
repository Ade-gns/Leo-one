/**
 * remoteDesktopKeys.ts — Mappe KeyboardEvent.code (déjà indépendant du
 * layout clavier) vers l'énumération leo_rd_key_t du protocole binaire du
 * bureau à distance.
 *
 * CONTRAT DE WIRE PROTOCOL avec l'agent : ces valeurs numériques doivent
 * correspondre EXACTEMENT à agent/src/remote_desktop.h (leo_rd_key_t) — ne
 * jamais réordonner, seulement ajouter à la fin des deux côtés en même
 * temps.
 */
export const LEO_KEY_UNKNOWN = 0

const CODE_TO_KEY: Record<string, number> = {
  KeyA: 1, KeyB: 2, KeyC: 3, KeyD: 4, KeyE: 5, KeyF: 6, KeyG: 7,
  KeyH: 8, KeyI: 9, KeyJ: 10, KeyK: 11, KeyL: 12, KeyM: 13, KeyN: 14,
  KeyO: 15, KeyP: 16, KeyQ: 17, KeyR: 18, KeyS: 19, KeyT: 20, KeyU: 21,
  KeyV: 22, KeyW: 23, KeyX: 24, KeyY: 25, KeyZ: 26,

  Digit0: 27, Digit1: 28, Digit2: 29, Digit3: 30, Digit4: 31,
  Digit5: 32, Digit6: 33, Digit7: 34, Digit8: 35, Digit9: 36,

  Enter: 37, Escape: 38, Backspace: 39, Tab: 40, Space: 41,

  Minus: 42, Equal: 43, BracketLeft: 44, BracketRight: 45,
  Backslash: 46, Semicolon: 47, Quote: 48, Backquote: 49,
  Comma: 50, Period: 51, Slash: 52,

  CapsLock: 53,

  F1: 54, F2: 55, F3: 56, F4: 57, F5: 58, F6: 59,
  F7: 60, F8: 61, F9: 62, F10: 63, F11: 64, F12: 65,

  PrintScreen: 66, ScrollLock: 67, Pause: 68,
  Insert: 69, Home: 70, PageUp: 71, Delete: 72, End: 73, PageDown: 74,

  ArrowRight: 75, ArrowLeft: 76, ArrowDown: 77, ArrowUp: 78,

  NumLock: 79,

  ControlLeft: 80, ShiftLeft: 81, AltLeft: 82, MetaLeft: 83,
  ControlRight: 84, ShiftRight: 85, AltRight: 86, MetaRight: 87,
}

/** Retourne le code leo_rd_key_t pour un KeyboardEvent.code donné, ou
 *  LEO_KEY_UNKNOWN (0) si la touche n'est pas supportée — l'appelant doit
 *  alors s'abstenir d'envoyer le message plutôt que d'envoyer 0 en aveugle
 *  (0 est une valeur valide côté agent, "touche inconnue", pas un no-op). */
export function leoKeyForCode(code: string): number | null {
  return CODE_TO_KEY[code] ?? null
}
