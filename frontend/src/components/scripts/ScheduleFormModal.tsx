/**
 * ScheduleFormModal.tsx — Création ou modification d'une planification
 * (script + cible + minutage : récurrent par cron, ou ponctuel à une
 * date/heure précise)
 */
import { useState } from 'react'
import { X, CalendarClock, Loader2, Save, Users as UsersIcon, Layers } from 'lucide-react'
import { useScripts, useCreateSchedule, useUpdateSchedule } from '@/hooks/useScripts'
import { useAgents } from '@/hooks/useAgents'
import { useWorkspaces } from '@/hooks/useWorkspaces'
import { parseCronToPreset, buildCronFromPreset, WEEKDAYS } from '@/lib/cron'
import type { RecurrencePreset } from '@/lib/cron'
import { cn } from '@/lib/utils'
import type { ScriptSchedule } from '@/types/script'

interface ScheduleFormModalProps {
  schedule?: ScriptSchedule  // présent = édition, absent = création
  onClose:   () => void
}

const PRESETS: { value: RecurrencePreset; label: string }[] = [
  { value: 'hourly',  label: 'Toutes les heures' },
  { value: 'daily',   label: 'Tous les jours'     },
  { value: 'weekly',  label: 'Chaque semaine'     },
  { value: 'advanced', label: 'Avancé (cron)'     },
]

/** ISO 8601 → valeur d'un <input type="datetime-local"> ("YYYY-MM-DDTHH:mm"),
 *  dans le fuseau horaire local du navigateur. */
function isoToDatetimeLocal(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Une heure dans ~5 minutes, arrondie à la minute — valeur par défaut
 *  raisonnable pour "exécuter une fois" en création. */
function defaultRunAtLocal(): string {
  const d = new Date(Date.now() + 5 * 60_000)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function ScheduleFormModal({ schedule, onClose }: ScheduleFormModalProps) {
  const isEdit = !!schedule
  const initialRecurrence = parseCronToPreset(schedule?.cron_expression ?? '0 2 * * *')

  const [name, setName]           = useState(schedule?.name ?? '')
  const [scriptId, setScriptId]   = useState(schedule?.script_id ?? '')
  const [targetMode, setTargetMode] = useState<'agent' | 'workspace'>(
    schedule?.workspace_id ? 'workspace' : 'agent',
  )
  const [agentId, setAgentId]         = useState(schedule?.agent_id ?? '')
  const [workspaceId, setWorkspaceId] = useState(schedule?.workspace_id ?? '')

  // Mode "récurrent" (cron) vs "une fois" (date/heure précise) — déduit de
  // la planification existante en édition (run_at renseigné = ponctuelle).
  const [scheduleMode, setScheduleMode] = useState<'recurring' | 'once'>(
    schedule?.run_at ? 'once' : 'recurring',
  )
  const [runAtLocal, setRunAtLocal] = useState(
    schedule?.run_at ? isoToDatetimeLocal(schedule.run_at) : defaultRunAtLocal(),
  )

  const [preset, setPreset]           = useState<RecurrencePreset>(initialRecurrence.preset)
  const [time, setTime]               = useState(initialRecurrence.time)
  const [weekday, setWeekday]         = useState(initialRecurrence.weekday)
  const [advancedCron, setAdvancedCron] = useState(schedule?.cron_expression ?? '0 2 * * *')
  const [timeoutSec, setTimeoutSec]   = useState(schedule?.timeout_sec ?? 60)
  const [error, setError]             = useState<string | null>(null)

  const { data: scriptsData } = useScripts()
  const scripts = scriptsData?.data ?? []
  const { data: agentsData } = useAgents()
  const agents = agentsData?.data ?? []
  const { data: workspacesData } = useWorkspaces()
  const workspaces = workspacesData?.data ?? []

  const createSchedule = useCreateSchedule()
  const updateSchedule = useUpdateSchedule()
  const isPending = createSchedule.isPending || updateSchedule.isPending

  const canSubmit =
    name.trim() !== '' &&
    scriptId !== '' &&
    (targetMode === 'agent' ? agentId !== '' : workspaceId !== '') &&
    (scheduleMode === 'once'
      ? runAtLocal !== '' && new Date(runAtLocal).getTime() > Date.now()
      : preset !== 'advanced' || advancedCron.trim() !== '')

  const handleSubmit = () => {
    if (!canSubmit) return
    setError(null)

    const payload = {
      script_id:    scriptId,
      name:         name.trim(),
      agent_id:     targetMode === 'agent' ? agentId : undefined,
      workspace_id: targetMode === 'workspace' ? workspaceId : undefined,
      timeout_sec:  timeoutSec,
      ...(scheduleMode === 'once'
        ? { run_at: new Date(runAtLocal).toISOString() }
        : { cron_expression: buildCronFromPreset({ preset, time, weekday }, advancedCron.trim()) }),
    }
    const onError = (err: unknown) => setError(err instanceof Error ? err.message : 'Erreur inconnue')

    if (isEdit) {
      updateSchedule.mutate({ scheduleID: schedule.id, payload }, { onSuccess: onClose, onError })
    } else {
      createSchedule.mutate(payload, { onSuccess: onClose, onError })
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-lg max-h-[90vh] flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <CalendarClock className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              {isEdit ? 'Modifier la planification' : 'Nouvelle planification'}
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 overflow-y-auto p-6">
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="ex : Nettoyage nocturne"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Script
            </label>
            <select
              value={scriptId}
              onChange={e => setScriptId(e.target.value)}
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
            >
              <option value="">Choisir un script…</option>
              {scripts.map(s => (
                <option key={s.id} value={s.id}>{s.name} ({s.interpreter})</option>
              ))}
            </select>
          </div>

          {/* Ciblage */}
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Cible
            </label>
            <div className="flex gap-2">
              <button
                onClick={() => setTargetMode('agent')}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  targetMode === 'agent' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                <UsersIcon className="h-4 w-4" />
                Un agent
              </button>
              <button
                onClick={() => setTargetMode('workspace')}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  targetMode === 'workspace' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                <Layers className="h-4 w-4" />
                Workspace entier
              </button>
            </div>

            {targetMode === 'agent' ? (
              <select
                value={agentId}
                onChange={e => setAgentId(e.target.value)}
                className="mt-2 w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
              >
                <option value="">Choisir une machine…</option>
                {agents.map(a => (
                  <option key={a.id} value={a.id}>{a.hostname}</option>
                ))}
              </select>
            ) : (
              <select
                value={workspaceId}
                onChange={e => setWorkspaceId(e.target.value)}
                className="mt-2 w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
              >
                <option value="">Choisir un workspace…</option>
                {workspaces.map(w => (
                  <option key={w.id} value={w.id}>{w.name}</option>
                ))}
              </select>
            )}
          </div>

          {/* Minutage : récurrent (cron) ou une seule fois (date/heure) */}
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Minutage
            </label>
            <div className="flex gap-2">
              <button
                onClick={() => setScheduleMode('recurring')}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  scheduleMode === 'recurring' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                Récurrent
              </button>
              <button
                onClick={() => setScheduleMode('once')}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  scheduleMode === 'once' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                Une seule fois
              </button>
            </div>

            {scheduleMode === 'once' ? (
              <input
                type="datetime-local"
                value={runAtLocal}
                onChange={e => setRunAtLocal(e.target.value)}
                className="mt-2 w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />
            ) : (
              <>
                <div className="mt-2 flex flex-wrap gap-2">
                  {PRESETS.map(p => (
                    <button
                      key={p.value}
                      onClick={() => setPreset(p.value)}
                      className={cn(
                        'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                        preset === p.value ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                      )}
                    >
                      {p.label}
                    </button>
                  ))}
                </div>

                {(preset === 'daily' || preset === 'weekly') && (
                  <div className="mt-2 flex items-center gap-2">
                    {preset === 'weekly' && (
                      <select
                        value={weekday}
                        onChange={e => setWeekday(Number(e.target.value))}
                        className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
                      >
                        {WEEKDAYS.map(d => (
                          <option key={d.value} value={d.value}>{d.label}</option>
                        ))}
                      </select>
                    )}
                    <span className="text-sm text-gray-500">à</span>
                    <input
                      type="time"
                      value={time}
                      onChange={e => setTime(e.target.value)}
                      className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
                    />
                  </div>
                )}

                {preset === 'advanced' && (
                  <input
                    type="text"
                    value={advancedCron}
                    onChange={e => setAdvancedCron(e.target.value)}
                    placeholder="0 2 * * *"
                    className="mt-2 w-full rounded-lg border border-gray-200 px-3 py-2 font-mono text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                  />
                )}
              </>
            )}
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Timeout d'exécution (secondes)
            </label>
            <input
              type="number"
              min={1}
              value={timeoutSec}
              onChange={e => setTimeoutSec(Number(e.target.value))}
              className="w-32 rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          {error && <p className="text-xs text-red-500">Erreur : {error}</p>}

          <div className="flex justify-end">
            <button
              onClick={handleSubmit}
              disabled={!canSubmit || isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Enregistrement…</>
                : <><Save className="h-4 w-4" />{isEdit ? 'Enregistrer' : 'Créer'}</>
              }
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
