/**
 * WorkspacesPage.tsx — Gestion des workspaces (regroupements de machines)
 */
import { useState } from 'react'
import { Boxes, Plus } from 'lucide-react'
import { WorkspaceTable } from '@/components/workspaces/WorkspaceTable'
import { WorkspaceFormModal } from '@/components/workspaces/WorkspaceFormModal'

export default function WorkspacesPage() {
  const [showCreateModal, setShowCreateModal] = useState(false)

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Boxes className="h-6 w-6 text-brand-600" />
          <div>
            <h1 className="text-xl font-bold text-gray-900">Workspaces</h1>
            <p className="text-sm text-gray-500 mt-0.5">Regroupements de machines au sein du tenant</p>
          </div>
        </div>

        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-700"
        >
          <Plus className="h-4 w-4" />
          Nouveau workspace
        </button>
      </div>

      <WorkspaceTable />

      {showCreateModal && (
        <WorkspaceFormModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}
