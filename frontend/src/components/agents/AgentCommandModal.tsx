/**
 * AgentCommandModal.tsx — Modal pour exécuter un script/commande sur un agent
 */
import { useState } from 'react'
import { X, Terminal, Loader2 } from 'lucide-react'
import { useExecScript } from '@/hooks/useAgents'
import type { ExecScriptPayload } from '@/types/agent'
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
  powershell: 'Write-Output "Hello from Leo-One"',
  cmd:        'echo Hello from Leo-One',
  python:     'print("Hello from Leo-One")',
}

interface AgentCommandModalProps {
  agentId:  string
  hostname: string
  onClose:  () => void
}

export function AgentCommandModal({ agentId, hostname, onClose }: AgentCommandModalProps) {
  const [interpreter, setInterpreter] = useState<Interpreter>('bash')
  const [script, setScript]           = useState('')
  const [output, setOutput]           = useState<string | null>(null)

  const execScript = useExecScript(agentId)

  const handleRun = () => {
    if (!script.trim()) return
    setOutput(null)
    const payload: ExecScriptPayload = { interpreter, script, timeout_secs: 60 }
    execScript.mutate(payload, {
      onSuccess: data => {
        setOutput(data.data?.stdout ?? data.data?.stderr ?? '(aucune sortie)')
      },
      onError: err => {
        setOutput(`Erreur : ${err instanceof Error ? err.message : 'inconnue'}`)
      },
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-2xl flex-col rounded-2xl bg-white shadow-2xl">

        {/* En-tête */}
        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <Terminal className="h-5 w-5 text-brand-600" />
            <div>
              <h2 className="text-base font-semibold text-gray-900">Exécuter un script</h2>
              <p className="text-xs text-gray-400">{hostname}</p>
            </div>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">

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

          {/* Bouton Run */}
          <div className="flex justify-end">
            <button
              onClick={handleRun}
              disabled={!script.trim() || execScript.isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {execScript.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Exécution…</>
                : <><Terminal className="h-4 w-4" />Exécuter</>
              }
            </button>
          </div>

          {/* Sortie */}
          {output !== null && (
            <div className="rounded-lg border border-gray-200 bg-gray-950 p-4">
              <p className="mb-1 text-xs font-semibold text-gray-400 uppercase tracking-wider">Sortie</p>
              <pre className="whitespace-pre-wrap text-xs text-green-400 font-mono max-h-48 overflow-y-auto">
                {output}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
