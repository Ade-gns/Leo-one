/**
 * BulkCommandModal.tsx — Exécuter un script sur plusieurs machines à la fois
 * (les machines sélectionnées, ou un workspace entier)
 */
import { useState } from 'react'
import { X, Terminal, Loader2, Users, Layers } from 'lucide-react'
import { useBulkExecScript } from '@/hooks/useAgents'
import { useWorkspaces } from '@/hooks/useWorkspaces'
import type { ExecScriptPayload, BulkCommandResult } from '@/types/agent'
import { cn } from '@/lib/utils'

type Interpreter = ExecScriptPayload['interpreter']

const SHELLS: { value: Interpreter; label: string }[] = [
  { value: 'bash',        label: 'Bash'       },
  { value: 'powershell',  label: 'PowerShell' },
  { value: 'cmd',         label: 'CMD'        },
  { value: 'python',      label: 'Python'     },
]

const PLACEHOLDERS: Record<Interpreter, string> = {
  bash:       '#!/bin/bash\necho "Hello from Leo-One"',
  sh:         '#!/bin/sh\necho "Hello from Leo-One"',
  powershell: 'Write-Output "Hello from Leo-One"',
  cmd:        'echo Hello from Leo-One',
  python:     'print("Hello from Leo-One")',
}

interface BulkCommandModalProps {
  /** Machines présélectionnées (ex : cases cochées dans la table) */
  selectedAgentIds: string[]
  onClose: () => void
}

export function BulkCommandModal({ selectedAgentIds, onClose }: BulkCommandModalProps) {
  const [targetMode, setTargetMode] = useState<'selection' | 'workspace'>(
    selectedAgentIds.length > 0 ? 'selection' : 'workspace',
  )
  const [workspaceId, setWorkspaceId] = useState('')
  const [interpreter, setInterpreter] = useState<Interpreter>('bash')
  const [script, setScript]           = useState('')
  const [results, setResults]         = useState<BulkCommandResult[] | null>(null)

  const { data: workspacesData } = useWorkspaces()
  const workspaces = workspacesData?.data ?? []

  const bulkExec = useBulkExecScript()

  const canSubmit =
    script.trim() !== '' &&
    (targetMode === 'selection' ? selectedAgentIds.length > 0 : workspaceId !== '')

  const handleRun = () => {
    if (!canSubmit) return
    setResults(null)

    const payload =
      targetMode === 'selection'
        ? { agent_ids: selectedAgentIds, interpreter, script, timeout_sec: 60 }
        : { workspace_id: workspaceId, interpreter, script, timeout_sec: 60 }

    bulkExec.mutate(payload, {
      onSuccess: data => setResults(data.data ?? []),
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-2xl flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <Terminal className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">Exécuter un script sur plusieurs machines</h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">

          {/* Ciblage */}
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Cible
            </label>
            <div className="flex gap-2">
              <button
                onClick={() => setTargetMode('selection')}
                disabled={selectedAgentIds.length === 0}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                  targetMode === 'selection' ? 'bg-brand-900 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                <Users className="h-4 w-4" />
                {selectedAgentIds.length} machine{selectedAgentIds.length > 1 ? 's' : ''} sélectionnée{selectedAgentIds.length > 1 ? 's' : ''}
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

          {/* Sélecteur d'interpréteur */}
          <div className="flex items-center gap-2">
            {SHELLS.map(s => (
              <button
                key={s.value}
                onClick={() => setInterpreter(s.value)}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  interpreter === s.value
                    ? 'bg-brand-900 text-white'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                )}
              >
                {s.label}
              </button>
            ))}
          </div>

          {/* Éditeur de script */}
          <textarea
            value={script}
            onChange={e => setScript(e.target.value)}
            placeholder={PLACEHOLDERS[interpreter]}
            rows={8}
            className="w-full resize-none rounded-lg border border-gray-200 bg-gray-950 px-4 py-3 font-mono text-sm text-green-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
          />

          <div className="flex justify-end">
            <button
              onClick={handleRun}
              disabled={!canSubmit || bulkExec.isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {bulkExec.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Envoi…</>
                : <><Terminal className="h-4 w-4" />Exécuter</>
              }
            </button>
          </div>

          {/* Résumé d'envoi — le résultat détaillé (stdout/stderr) de chaque
              machine reste consultable sur sa fiche, dans l'historique des
              commandes (déjà rafraîchi automatiquement toutes les 5s). */}
          {results !== null && (
            <div className="rounded-lg border border-gray-200 p-4">
              <p className="mb-2 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                Envoyé à {results.filter(r => r.sent).length}/{results.length} machine{results.length > 1 ? 's' : ''}
                {results.some(r => !r.sent && !r.error) && ' (les machines hors ligne recevront la commande à leur reconnexion)'}
              </p>
              <ul className="flex flex-col gap-1 max-h-40 overflow-y-auto text-xs">
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
