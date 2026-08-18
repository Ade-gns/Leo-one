/**
 * FileUploadModal.tsx — Upload d'un nouveau fichier dans la bibliothèque
 */
import { useState } from 'react'
import { X, Upload, Loader2, UploadCloud } from 'lucide-react'
import { useUploadFile } from '@/hooks/useFiles'
import { formatBytes } from '@/lib/utils'

interface FileUploadModalProps {
  onClose: () => void
}

export function FileUploadModal({ onClose }: FileUploadModalProps) {
  const [file, setFile] = useState<File | null>(null)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const uploadFile = useUploadFile()

  const canSubmit = file !== null

  const handleFileSelect = (f: File | undefined) => {
    if (!f) return
    setFile(f)
    if (!name.trim()) setName(f.name)
  }

  const handleSubmit = () => {
    if (!file) return
    setError(null)
    uploadFile.mutate(
      { file, name: name.trim() || undefined },
      {
        onSuccess: onClose,
        onError: err => setError(err instanceof Error ? err.message : 'Erreur inconnue'),
      },
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-md flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <UploadCloud className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">Uploader un fichier</h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">
          <label
            className="flex cursor-pointer flex-col items-center gap-2 rounded-lg border-2 border-dashed border-gray-200 px-4 py-8 text-center hover:border-brand-400 hover:bg-brand-50/30"
          >
            <UploadCloud className="h-8 w-8 text-gray-300" />
            {file ? (
              <div>
                <p className="text-sm font-medium text-gray-800">{file.name}</p>
                <p className="text-xs text-gray-400">{formatBytes(file.size)}</p>
              </div>
            ) : (
              <p className="text-sm text-gray-500">Cliquer pour choisir un fichier</p>
            )}
            <input
              type="file"
              className="hidden"
              onChange={e => handleFileSelect(e.target.files?.[0])}
            />
          </label>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom (dans la bibliothèque)
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="ex : installer-agent-v1.2.msi"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          {error && <p className="text-xs text-red-500">Erreur : {error}</p>}

          <div className="flex justify-end">
            <button
              onClick={handleSubmit}
              disabled={!canSubmit || uploadFile.isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {uploadFile.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Envoi…</>
                : <><Upload className="h-4 w-4" />Uploader</>
              }
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
