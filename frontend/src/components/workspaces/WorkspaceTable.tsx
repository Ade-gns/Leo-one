/**
 * WorkspaceTable.tsx — Table des workspaces du tenant courant
 */
import { useState } from 'react'
import { Boxes, Pencil, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useWorkspaces, useDeleteWorkspace } from '@/hooks/useWorkspaces'
import { WorkspaceFormModal } from './WorkspaceFormModal'
import type { Workspace } from '@/types/workspace'

export function WorkspaceTable() {
  const { data, isLoading } = useWorkspaces()
  const deleteWorkspace = useDeleteWorkspace()

  const [editingWorkspace, setEditingWorkspace] = useState<Workspace | null>(null)

  const workspaces = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Description</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Créé</th>
              <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              Array.from({ length: 3 }).map((_, i) => (
                <tr key={i} className="border-b border-gray-50">
                  {Array.from({ length: 4 }).map((_, j) => (
                    <td key={j} className="px-4 py-3">
                      <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                    </td>
                  ))}
                </tr>
              ))
            )}

            {!isLoading && workspaces.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-12 text-center text-gray-400">
                  <Boxes className="mx-auto h-8 w-8 mb-2 opacity-40" />
                  Aucun workspace — les machines sans workspace restent visibles dans Machines
                </td>
              </tr>
            )}

            {!isLoading && workspaces.map(ws => (
              <tr key={ws.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3 font-medium text-gray-900">{ws.name}</td>
                <td className="px-4 py-3 text-gray-500">{ws.description || '—'}</td>
                <td className="px-4 py-3 text-gray-400 text-xs">
                  {formatDistanceToNow(new Date(ws.created_at), { addSuffix: true, locale: fr })}
                </td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                      title="Modifier"
                      onClick={() => setEditingWorkspace(ws)}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                      title="Supprimer"
                      onClick={() => {
                        if (confirm(`Supprimer le workspace "${ws.name}" ? Les machines rattachées ne seront pas supprimées.`)) {
                          deleteWorkspace.mutate(ws.id)
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
            {workspaces.length} workspace{workspaces.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {editingWorkspace && (
        <WorkspaceFormModal workspace={editingWorkspace} onClose={() => setEditingWorkspace(null)} />
      )}
    </div>
  )
}
