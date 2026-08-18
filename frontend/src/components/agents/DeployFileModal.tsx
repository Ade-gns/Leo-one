/**
 * DeployFileModal.tsx — Déployer un fichier de la bibliothèque sur un agent,
 * avec barre de progression pendant le transfert (poll de la commande créée)
 */
import { useState } from 'react'
import { X, FolderOpen, Loader2, Send } from 'lucide-react'
import { useFiles, useDeployFile } from '@/hooks/useFiles'
import { useAgentCommand } from '@/hooks/useAgents'
import { cn } from '@/lib/utils'

interface DeployFileModalProps {
  agentId: string
  hostname: string
  onClose: () => void
}

export function DeployFileModal({ agentId, hostname, onClose }: DeployFileModalProps) {
  const [fileId, setFileId] = useState('')
  const [commandId, setCommandId] = useState<string | null>(null)

  const { data: filesData } = useFiles()
  const files = filesData?.data ?? []

  const deployFile = useDeployFile(agentId)
  const { data: cmdData } = useAgentCommand(agentId, commandId)
  const command = cmdData?.data

  const canSubmit = fileId !== '' && !commandId

  const handleDeploy = () => {
    if (!canSubmit) return
    deployFile.mutate({ file_id: fileId }, {
      onSuccess: data => setCommandId(data.data.command_id),
    })
  }

  const percent = command?.progress_percent ?? 0
  const isDone = command?.status === 'success' || command?.status === 'failed' || command?.status === 'timeout'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-md flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <FolderOpen className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">Déployer un fichier vers « {hostname} »</h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">
          {!commandId && (
            <div>
              <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Fichier
              </label>
              <select
                value={fileId}
                onChange={e => setFileId(e.target.value)}
                className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
              >
                <option value="">Choisir un fichier…</option>
                {files.map(f => (
                  <option key={f.id} value={f.id}>{f.name}</option>
                ))}
              </select>
              {files.length === 0 && (
                <p className="mt-1.5 text-xs text-gray-400">
                  Aucun fichier dans la bibliothèque — ajoutez-en un depuis la page « Fichiers ».
                </p>
              )}
            </div>
          )}

          {deployFile.isError && (
            <p className="text-xs text-red-500">
              Erreur : {deployFile.error instanceof Error ? deployFile.error.message : 'Erreur inconnue'}
            </p>
          )}

          {commandId && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between text-xs font-medium text-gray-500">
                <span>
                  {command?.status === 'pending' && 'En attente…'}
                  {command?.status === 'running' && 'Transfert en cours…'}
                  {command?.status === 'success' && 'Terminé'}
                  {(command?.status === 'failed' || command?.status === 'timeout') && 'Échec'}
                </span>
                <span>{percent}%</span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-gray-100">
                <div
                  className={cn(
                    'h-full rounded-full transition-all duration-300',
                    command?.status === 'failed' || command?.status === 'timeout' ? 'bg-red-500' : 'bg-brand-600',
                  )}
                  style={{ width: `${percent}%` }}
                />
              </div>
              {isDone && command?.status !== 'success' && command?.stderr && (
                <p className="text-xs text-red-500">{command.stderr}</p>
              )}
              {isDone && command?.status === 'success' && (
                <p className="text-xs text-green-600">Fichier déployé avec succès.</p>
              )}
            </div>
          )}

          <div className="flex justify-end">
            {!commandId ? (
              <button
                onClick={handleDeploy}
                disabled={!canSubmit || deployFile.isPending}
                className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {deployFile.isPending
                  ? <><Loader2 className="h-4 w-4 animate-spin" />Envoi…</>
                  : <><Send className="h-4 w-4" />Déployer</>
                }
              </button>
            ) : (
              <button
                onClick={onClose}
                className="rounded-lg border border-gray-200 px-5 py-2.5 text-sm font-semibold text-gray-700 hover:bg-gray-50"
              >
                Fermer
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
