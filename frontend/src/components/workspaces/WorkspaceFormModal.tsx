/**
 * WorkspaceFormModal.tsx — Création ou modification d'un workspace
 */
import { useState } from 'react'
import { X, Boxes, Loader2, Save } from 'lucide-react'
import { useCreateWorkspace, useUpdateWorkspace } from '@/hooks/useWorkspaces'
import type { Workspace } from '@/types/workspace'

interface WorkspaceFormModalProps {
  workspace?: Workspace  // présent = édition, absent = création
  onClose:    () => void
}

export function WorkspaceFormModal({ workspace, onClose }: WorkspaceFormModalProps) {
  const isEdit = !!workspace

  const [name, setName]               = useState(workspace?.name ?? '')
  const [description, setDescription] = useState(workspace?.description ?? '')
  const [error, setError]             = useState<string | null>(null)

  const createWorkspace = useCreateWorkspace()
  const updateWorkspace = useUpdateWorkspace()
  const isPending = createWorkspace.isPending || updateWorkspace.isPending

  const canSubmit = name.trim() !== ''

  const handleSubmit = () => {
    if (!canSubmit) return
    setError(null)

    const payload = { name: name.trim(), description: description.trim() || undefined }
    const onError = (err: unknown) => setError(err instanceof Error ? err.message : 'Erreur inconnue')

    if (isEdit) {
      updateWorkspace.mutate({ workspaceID: workspace.id, payload }, { onSuccess: onClose, onError })
    } else {
      createWorkspace.mutate(payload, { onSuccess: onClose, onError })
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-md flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <Boxes className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              {isEdit ? 'Modifier le workspace' : 'Nouveau workspace'}
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="ex : Paris, Client Acme, Étage 3…"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Description (optionnel)
            </label>
            <textarea
              value={description}
              onChange={e => setDescription(e.target.value)}
              rows={3}
              className="w-full resize-none rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
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
