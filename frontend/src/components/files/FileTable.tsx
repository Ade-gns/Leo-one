/**
 * FileTable.tsx — Table de la bibliothèque de fichiers déployables du tenant courant
 */
import { Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useFiles, useDeleteFile } from '@/hooks/useFiles'
import { formatBytes } from '@/lib/utils'

export function FileTable() {
  const { data, isLoading } = useFiles()
  const deleteFile = useDeleteFile()

  const files = data?.data ?? []

  return (
    <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-gray-100 bg-gray-50">
            <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
            <th className="px-4 py-3 text-left font-semibold text-gray-600">Taille</th>
            <th className="px-4 py-3 text-left font-semibold text-gray-600">SHA-256</th>
            <th className="px-4 py-3 text-left font-semibold text-gray-600">Ajouté</th>
            <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
          </tr>
        </thead>
        <tbody>
          {isLoading && (
            Array.from({ length: 3 }).map((_, i) => (
              <tr key={i} className="border-b border-gray-50">
                {Array.from({ length: 5 }).map((_, j) => (
                  <td key={j} className="px-4 py-3">
                    <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                  </td>
                ))}
              </tr>
            ))
          )}

          {!isLoading && files.length === 0 && (
            <tr>
              <td colSpan={5} className="px-4 py-12 text-center text-gray-400">
                Aucun fichier dans la bibliothèque
              </td>
            </tr>
          )}

          {!isLoading && files.map(f => (
            <tr key={f.id} className="border-b border-gray-50 hover:bg-gray-50">
              <td className="px-4 py-3 font-medium text-gray-900">{f.name}</td>
              <td className="px-4 py-3 text-gray-500">{formatBytes(f.size_bytes)}</td>
              <td className="px-4 py-3 font-mono text-xs text-gray-400" title={f.checksum_sha256}>
                {f.checksum_sha256.slice(0, 12)}…
              </td>
              <td className="px-4 py-3 text-gray-500">
                {formatDistanceToNow(new Date(f.created_at), { addSuffix: true, locale: fr })}
              </td>
              <td className="px-4 py-3 text-right">
                <button
                  className="rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                  title="Supprimer"
                  onClick={() => {
                    if (confirm(`Supprimer le fichier "${f.name}" de la bibliothèque ?`)) {
                      deleteFile.mutate(f.id)
                    }
                  }}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {!isLoading && (
        <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
          {files.length} fichier{files.length > 1 ? 's' : ''}
        </div>
      )}
    </div>
  )
}
