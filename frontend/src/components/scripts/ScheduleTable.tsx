/**
 * ScheduleTable.tsx — Table des planifications récurrentes du tenant courant
 */
import { useState } from 'react'
import { Pencil, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useSchedules, useDeleteSchedule, useUpdateSchedule } from '@/hooks/useScripts'
import { useScripts } from '@/hooks/useScripts'
import { useAgents } from '@/hooks/useAgents'
import { useWorkspaces } from '@/hooks/useWorkspaces'
import { describeCron } from '@/lib/cron'
import { cn } from '@/lib/utils'
import { ScheduleFormModal } from './ScheduleFormModal'
import type { ScriptSchedule } from '@/types/script'

/** Description en français lisible du minutage d'une planification —
 *  ponctuelle (run_at) ou récurrente (cron_expression, voir describeCron). */
function describeSchedule(s: ScriptSchedule): string {
  if (s.run_at) {
    return `Une fois le ${new Date(s.run_at).toLocaleString('fr-FR', {
      dateStyle: 'medium', timeStyle: 'short',
    })}`
  }
  return describeCron(s.cron_expression ?? '')
}

export function ScheduleTable() {
  const { data, isLoading } = useSchedules()
  const { data: scriptsData } = useScripts()
  const { data: agentsData } = useAgents()
  const { data: workspacesData } = useWorkspaces()
  const deleteSchedule = useDeleteSchedule()
  const updateSchedule = useUpdateSchedule()

  const [editingSchedule, setEditingSchedule] = useState<ScriptSchedule | null>(null)

  const schedules   = data?.data ?? []
  const scriptNames  = new Map((scriptsData?.data ?? []).map(s => [s.id, s.name]))
  const agentNames   = new Map((agentsData?.data ?? []).map(a => [a.id, a.hostname]))
  const workspaceNames = new Map((workspacesData?.data ?? []).map(w => [w.id, w.name]))

  const targetLabel = (s: ScriptSchedule) =>
    s.agent_id
      ? (agentNames.get(s.agent_id) ?? 'Machine inconnue')
      : `Workspace : ${workspaceNames.get(s.workspace_id ?? '') ?? 'inconnu'}`

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Script</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Cible</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Récurrence</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Prochaine exécution</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Actif</th>
              <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              Array.from({ length: 3 }).map((_, i) => (
                <tr key={i} className="border-b border-gray-50">
                  {Array.from({ length: 7 }).map((_, j) => (
                    <td key={j} className="px-4 py-3">
                      <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                    </td>
                  ))}
                </tr>
              ))
            )}

            {!isLoading && schedules.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-12 text-center text-gray-400">
                  Aucune planification
                </td>
              </tr>
            )}

            {!isLoading && schedules.map(s => (
              <tr key={s.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3 font-medium text-gray-900">{s.name}</td>
                <td className="px-4 py-3 text-gray-500">{scriptNames.get(s.script_id) ?? '—'}</td>
                <td className="px-4 py-3 text-gray-500">{targetLabel(s)}</td>
                <td className="px-4 py-3 text-gray-500">{describeSchedule(s)}</td>
                <td className="px-4 py-3 text-gray-400 text-xs">
                  {formatDistanceToNow(new Date(s.next_run_at), { addSuffix: true, locale: fr })}
                </td>
                <td className="px-4 py-3">
                  <button
                    onClick={() => updateSchedule.mutate({ scheduleID: s.id, payload: { enabled: !s.enabled } })}
                    className={cn(
                      'relative h-5 w-9 rounded-full transition-colors',
                      s.enabled ? 'bg-brand-600' : 'bg-gray-200',
                    )}
                    title={s.enabled ? 'Désactiver' : 'Activer'}
                  >
                    <span
                      className={cn(
                        'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
                        s.enabled ? 'translate-x-4' : 'translate-x-0.5',
                      )}
                    />
                  </button>
                </td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                      title="Modifier"
                      onClick={() => setEditingSchedule(s)}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                      title="Supprimer"
                      onClick={() => {
                        if (confirm(`Supprimer la planification "${s.name}" ?`)) {
                          deleteSchedule.mutate(s.id)
                        }
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!isLoading && (
          <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
            {schedules.length} planification{schedules.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {editingSchedule && (
        <ScheduleFormModal schedule={editingSchedule} onClose={() => setEditingSchedule(null)} />
      )}
    </div>
  )
}
