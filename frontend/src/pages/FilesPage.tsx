/**
 * FilesPage.tsx — Bibliothèque de fichiers déployables (installeurs, configs, ...)
 */
import { useState } from 'react'
import { FolderOpen, Plus } from 'lucide-react'
import { FileTable } from '@/components/files/FileTable'
import { FileUploadModal } from '@/components/files/FileUploadModal'

export default function FilesPage() {
  const [showUploadModal, setShowUploadModal] = useState(false)

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <FolderOpen className="h-6 w-6 text-brand-600" />
          <div>
            <h1 className="text-xl font-bold text-gray-900">Fichiers</h1>
            <p className="text-sm text-gray-500 mt-0.5">Bibliothèque de fichiers déployables sur les machines</p>
          </div>
        </div>

        <button
          onClick={() => setShowUploadModal(true)}
          className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-700"
        >
          <Plus className="h-4 w-4" />
          Uploader un fichier
        </button>
      </div>

      <FileTable />

      {showUploadModal && (
        <FileUploadModal onClose={() => setShowUploadModal(false)} />
      )}
    </div>
  )
}
