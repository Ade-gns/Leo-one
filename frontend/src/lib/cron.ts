/**
 * cron.ts — Conversion entre une expression cron standard à 5 champs
 * (minute heure jour-du-mois mois jour-de-semaine) et une UI de récurrence
 * simplifiée par préréglages (toutes les heures / tous les jours / chaque
 * semaine). Une expression qui ne correspond à aucun de ces préréglages est
 * traitée comme "avancée" (éditée telle quelle).
 */

export type RecurrencePreset = 'hourly' | 'daily' | 'weekly' | 'advanced'

export const WEEKDAYS = [
  { value: 0, label: 'dimanche' },
  { value: 1, label: 'lundi' },
  { value: 2, label: 'mardi' },
  { value: 3, label: 'mercredi' },
  { value: 4, label: 'jeudi' },
  { value: 5, label: 'vendredi' },
  { value: 6, label: 'samedi' },
] as const

export interface RecurrenceState {
  preset:  RecurrencePreset
  time:    string   // "HH:MM", utilisé par daily/weekly
  weekday: number   // 0-6 (dimanche=0), utilisé par weekly
}

const pad2 = (n: number) => n.toString().padStart(2, '0')

/** Déduit un état de récurrence "simplifié" à partir d'une expression cron
 *  existante — utilisé pour préremplir le formulaire en édition. Retombe
 *  sur le mode "avancé" si l'expression ne correspond à aucun préréglage. */
export function parseCronToPreset(expr: string): RecurrenceState {
  const fallback: RecurrenceState = { preset: 'advanced', time: '02:00', weekday: 1 }
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return fallback

  const [min, hour, dom, month, dow] = parts
  if (dom !== '*' || month !== '*') return fallback

  if (min === '0' && hour === '*' && dow === '*') {
    return { preset: 'hourly', time: '02:00', weekday: 1 }
  }
  if (/^\d+$/.test(min) && /^\d+$/.test(hour) && dow === '*') {
    return { preset: 'daily', time: `${pad2(+hour)}:${pad2(+min)}`, weekday: 1 }
  }
  if (/^\d+$/.test(min) && /^\d+$/.test(hour) && /^[0-6]$/.test(dow)) {
    return { preset: 'weekly', time: `${pad2(+hour)}:${pad2(+min)}`, weekday: +dow }
  }
  return fallback
}

/** Construit l'expression cron envoyée au serveur à partir de l'état de
 *  récurrence simplifié. En mode "avancé", advancedValue est l'expression
 *  telle que saisie par l'utilisateur (retournée sans modification). */
export function buildCronFromPreset(state: RecurrenceState, advancedValue: string): string {
  if (state.preset === 'advanced') return advancedValue
  if (state.preset === 'hourly') return '0 * * * *'

  const [hh, mm] = state.time.split(':')
  const h = Number.isFinite(+hh) ? +hh : 0
  const m = Number.isFinite(+mm) ? +mm : 0

  if (state.preset === 'daily') return `${m} ${h} * * *`
  return `${m} ${h} * * ${state.weekday}`
}

/** Description en français lisible, pour l'affichage dans la liste des
 *  planifications. Retombe sur l'expression cron brute si elle ne
 *  correspond à aucun préréglage connu. */
export function describeCron(expr: string): string {
  const state = parseCronToPreset(expr)
  switch (state.preset) {
    case 'hourly': return 'Toutes les heures'
    case 'daily':  return `Tous les jours à ${state.time}`
    case 'weekly': return `Chaque ${WEEKDAYS[state.weekday].label} à ${state.time}`
    default:       return `Cron : ${expr}`
  }
}
