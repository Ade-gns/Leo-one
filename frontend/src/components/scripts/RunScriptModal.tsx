/**
 * RunScriptModal.tsx — Exécuter immédiatement un script de la bibliothèque
 * sur des machines sélectionnées ou un workspace entier (le contenu du
 * script n'est pas modifiable ici, seul le ciblage l'est — pour un envoi
 * ad-hoc avec édition libre du contenu, voir BulkCommandModal).
 */
import { useState } from 'react'
import { X, Play, Loader2, Users, Layers } from 'lucide-react'
import { useBulkExecScript } from '@/hooks/useAgents'
import { useAgents } from '@/hooks/useAgents'
import { useWorkspaces } from '@/hooks/useWorkspaces'
import type { BulkCommandResult } from '@/types/agent'
import type { Script } from '@/types/script'
import { cn } from '@/lib/utils'

interface RunScriptModalProps {
  script: Script
  onClose: () => void
}

export function RunScriptModal({ script, onClose }: RunScriptModalProps) {
  const [targetMode, setTargetMode] = useState<'selection' | 'workspace'>('selection')
  const [selected, setSelected]     = useState<Set<string>>(new Set())
  const [workspaceId, setWorkspaceId] = useState('')
  const [results, setResults]       = useState<BulkCommandResult[] | null>(null)

  const { data: agentsData } = useAgents()
  const { data: workspacesData } = useWorkspaces()
  const agents = agentsData?.data ?? []
  const workspaces = workspacesData?.data ?? []

  const bulkExec = useBulkExecScript()

  const toggleAgent = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const canSubmit =
    targetMode === 'selection' ? selected.size > 0 : workspaceId !== ''

  const handleRun = () => {
    if (!canSubmit) return
    setResults(null)

    const payload =
      targetMode === 'selection'
        ? { agent_ids: Array.from(selected), interpreter: script.interpreter, script: script.content, timeout_sec: 60 }
        : { workspace_id: workspaceId, interpreter: script.interpreter, script: script.content, timeout_sec: 60 }

    bulkExec.mutate(payload, {
      onSuccess: data => setResults(data.data ?? []),
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-lg flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <Play className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              Exécuter « {script.name} »
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Cible
            </label>
            <div className="flex gap-2">
              <button
                onClick={() => setTargetMode('selection')}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  targetMode === 'selection' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                <Users className="h-4 w-4" />
                Machines sélectionnées
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

            {targetMode === 'selection' && (
              <div className="mt-2 max-h-48 overflow-y-auto rounded-lg border border-gray-200">
                {agents.length === 0 && (
                  <p className="px-3 py-4 text-center text-sm text-gray-400">Aucune machine</p>
                )}
                {agents.map(a => (
                  <label
                    key={a.id}
                    className="flex cursor-pointer items-center gap-2 border-b border-gray-50 px-3 py-2 text-sm last:border-b-0 hover:bg-gray-50"
                  >
                    <input
                      type="checkbox"
                      checked={selected.has(a.id)}
                      onChange={() => toggleAgent(a.id)}
                      className="h-4 w-4 rounded border-gray-300 text-brand-600 focus:ring-brand-500"
                    />
                    <span className="text-gray-900">{a.hostname}</span>
                  </label>
                ))}
              </div>
            )}

            {targetMode === 'workspace' && (
              <select
                value={workspaceId}
                onChange={e => setWorkspaceId(e.target.value)}
                className="mt-2 w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              >
                <option value="">Choisir un workspace…</option>
                {workspaces.map(w => (
                  <option key={w.id} value={w.id}>{w.name}</option>
                ))}
              </select>
            )}
          </div>

          <div className="flex justify-end">
            <button
              onClick={handleRun}
              disabled={!canSubmit || bulkExec.isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {bulkExec.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Envoi…</>
                : <><Play className="h-4 w-4" />Exécuter</>
              }
            </button>
          </div>

          {results !== null && (
            <div className="rounded-lg border border-gray-200 p-4">
              <p className="mb-2 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                Envoyé à {results.filter(r => r.sent).length}/{results.length} machine{results.length > 1 ? 's' : ''}
                {results.some(r => !r.sent && !r.error) && ' (les machines hors ligne recevront la commande à leur reconnexion)'}
              </p>
              <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto text-xs">
                {results.map(r => (
                  <li key={r.agent_id} className="flex items-center justify-between">
                    <span className="font-mono text-gray-500">{r.agent_id}</span>
                    <span className={cn(
                      'font-medium',
                      r.error ? 'text-red-600' : r.sent ? 'text-green-600' : 'text-amber-600',
                    )}>
                      {r.error ? `Erreur : ${r.error}` : r.sent ? 'Envoyé' : 'En attente (hors ligne)'}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
