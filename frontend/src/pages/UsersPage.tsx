/**
 * UsersPage.tsx — Gestion des utilisateurs et des rôles du tenant
 */
import { useState } from 'react'
import { Users as UsersIcon, UserPlus, ShieldCheck, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { UserTable } from '@/components/users/UserTable'
import { UserFormModal } from '@/components/users/UserFormModal'
import { RoleTable } from '@/components/roles/RoleTable'
import { RoleFormModal } from '@/components/roles/RoleFormModal'

type Tab = 'users' | 'roles'

export default function UsersPage() {
  const [tab, setTab] = useState<Tab>('users')
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
          {tab === 'users' ? <UserPlus className="h-4 w-4" /> : <Plus className="h-4 w-4" />}
          {tab === 'users' ? 'Nouvel utilisateur' : 'Nouveau rôle'}
        </button>
      </div>

      <div className="flex gap-1 border-b border-gray-200">
        <button
          onClick={() => setTab('users')}
          className={cn(
            'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
            tab === 'users'
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-gray-500 hover:text-gray-700',
          )}
        >
          <UsersIcon className="h-4 w-4" />
          Utilisateurs
        </button>
        <button
          onClick={() => setTab('roles')}
          className={cn(
            'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
            tab === 'roles'
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-gray-500 hover:text-gray-700',
          )}
        >
          <ShieldCheck className="h-4 w-4" />
          Rôles
        </button>
      </div>

      {tab === 'users' ? <UserTable /> : <RoleTable />}

      {showCreateModal && tab === 'users' && (
        <UserFormModal onClose={() => setShowCreateModal(false)} />
      )}
      {showCreateModal && tab === 'roles' && (
        <RoleFormModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}
