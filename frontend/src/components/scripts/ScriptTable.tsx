/**
 * ScriptTable.tsx — Table de la bibliothèque de scripts du tenant courant
 */
import { useState } from 'react'
import { Pencil, Trash2, Play } from 'lucide-react'
import { useScripts, useDeleteScript } from '@/hooks/useScripts'
import { ScriptFormModal } from './ScriptFormModal'
import { RunScriptModal } from './RunScriptModal'
import type { Script } from '@/types/script'

export function ScriptTable() {
  const { data, isLoading } = useScripts()
  const deleteScript = useDeleteScript()

  const [editingScript, setEditingScript] = useState<Script | null>(null)
  const [runningScript, setRunningScript] = useState<Script | null>(null)

  const scripts = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Description</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Interpréteur</th>
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

            {!isLoading && scripts.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-12 text-center text-gray-400">
                  Aucun script dans la bibliothèque
                </td>
              </tr>
            )}

            {!isLoading && scripts.map(script => (
              <tr key={script.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3 font-medium text-gray-900">{script.name}</td>
                <td className="px-4 py-3 text-gray-500">{script.description || '—'}</td>
                <td className="px-4 py-3">
                  <span className="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600">
                    {script.interpreter}
                  </span>
                </td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                      title="Exécuter maintenant"
                      onClick={() => setRunningScript(script)}
                    >
                      <Play className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                      title="Modifier"
                      onClick={() => setEditingScript(script)}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                      title="Supprimer"
                      onClick={() => {
                        if (confirm(`Supprimer le script "${script.name}" ? Les planifications associées seront aussi supprimées.`)) {
                          deleteScript.mutate(script.id)
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
            {scripts.length} script{scripts.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {editingScript && (
        <ScriptFormModal script={editingScript} onClose={() => setEditingScript(null)} />
      )}
      {runningScript && (
        <RunScriptModal script={runningScript} onClose={() => setRunningScript(null)} />
      )}
    </div>
  )
}
