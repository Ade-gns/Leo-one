/**
 * UserTable.tsx — Table des utilisateurs du tenant courant
 */
import { useState } from 'react'
import { Users as UsersIcon, Pencil, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { cn } from '@/lib/utils'
import { useUsers, useDeleteUser } from '@/hooks/useUsers'
import { useAuthStore } from '@/store/auth.store'
import { UserFormModal } from './UserFormModal'
import type { User } from '@/types/user'

export function UserTable() {
  const { data, isLoading } = useUsers()
  const deleteUser = useDeleteUser()
  const currentUserID = useAuthStore(s => s.session?.user.id)

  const [editingUser, setEditingUser] = useState<User | null>(null)

  const users = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Email</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Rôles</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Statut</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Dernière connexion</th>
              <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              Array.from({ length: 4 }).map((_, i) => (
                <tr key={i} className="border-b border-gray-50">
                  {Array.from({ length: 6 }).map((_, j) => (
                    <td key={j} className="px-4 py-3">
                      <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                    </td>
                  ))}
                </tr>
              ))
            )}

            {!isLoading && users.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                  <UsersIcon className="mx-auto h-8 w-8 mb-2 opacity-40" />
                  Aucun utilisateur
                </td>
              </tr>
            )}

            {!isLoading && users.map(user => {
              const isSelf = user.id === currentUserID
              return (
                <tr key={user.id} className="border-b border-gray-50 hover:bg-gray-50">
                  <td className="px-4 py-3 font-medium text-gray-900">
                    {user.full_name}
                    {isSelf && <span className="ml-2 text-xs font-normal text-gray-400">(vous)</span>}
                  </td>
                  <td className="px-4 py-3 text-gray-500">{user.email}</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(user.roles ?? []).map(role => (
                        <span key={role.id} className="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600">
                          {role.name}
                        </span>
                      ))}
                      {(user.roles ?? []).length === 0 && <span className="text-xs text-gray-400">—</span>}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <span className={cn(
                      'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold',
                      user.is_active ? 'bg-green-50 text-green-700' : 'bg-gray-100 text-gray-600',
                    )}>
                      <span className={cn('h-1.5 w-1.5 rounded-full', user.is_active ? 'bg-green-500' : 'bg-gray-400')} />
                      {user.is_active ? 'Actif' : 'Désactivé'}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-gray-400 text-xs">
                    {user.last_login_at
                      ? formatDistanceToNow(new Date(user.last_login_at), { addSuffix: true, locale: fr })
                      : 'Jamais connecté'}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                        title="Modifier"
                        onClick={() => setEditingUser(user)}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        className={cn(
                          'rounded p-1.5',
                          isSelf
                            ? 'cursor-not-allowed text-gray-200'
                            : 'text-gray-400 hover:bg-red-50 hover:text-red-600',
                        )}
                        title={isSelf ? 'Impossible de se supprimer soi-même' : 'Supprimer'}
                        disabled={isSelf}
                        onClick={() => {
                          if (!isSelf && confirm(`Supprimer ${user.full_name} ?`)) {
                            deleteUser.mutate(user.id)
                          }
                        }}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>

        {!isLoading && (
          <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
            {users.length} utilisateur{users.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {editingUser && (
        <UserFormModal user={editingUser} onClose={() => setEditingUser(null)} />
      )}
    </div>
  )
}
