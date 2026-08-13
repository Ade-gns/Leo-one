/**
 * UsersPage.tsx — Gestion des utilisateurs du tenant
 */
import { useState } from 'react'
import { Users as UsersIcon, UserPlus } from 'lucide-react'
import { UserTable } from '@/components/users/UserTable'
import { UserFormModal } from '@/components/users/UserFormModal'

export default function UsersPage() {
  const [showCreateModal, setShowCreateModal] = useState(false)

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <UsersIcon className="h-6 w-6 text-brand-600" />
          <div>
            <h1 className="text-xl font-bold text-gray-900">Utilisateurs</h1>
            <p className="text-sm text-gray-500 mt-0.5">Gestion des comptes et des rôles</p>
          </div>
        </div>

        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-700"
        >
          <UserPlus className="h-4 w-4" />
          Nouvel utilisateur
        </button>
      </div>

      <UserTable />

      {showCreateModal && (
        <UserFormModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}
