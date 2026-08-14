/**
 * RoleTable.tsx — Table des rôles du tenant courant
 */
import { useState } from 'react'
import { Pencil, Trash2, Lock } from 'lucide-react'
import { useRoles, useDeleteRole } from '@/hooks/useRoles'
import { RoleFormModal } from './RoleFormModal'
import type { Role } from '@/types/user'

export function RoleTable() {
  const { data, isLoading } = useRoles()
  const deleteRole = useDeleteRole()

  const [editingRole, setEditingRole] = useState<Role | null>(null)

  const roles = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Nom</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Description</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Permissions</th>
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

            {!isLoading && roles.map(role => (
              <tr key={role.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3 font-medium text-gray-900">
                  <div className="flex items-center gap-1.5">
                    {role.name}
                    {role.is_system && (
                      <Lock className="h-3.5 w-3.5 text-gray-300" aria-label="Rôle système" />
                    )}
                  </div>
                </td>
                <td className="px-4 py-3 text-gray-500">{role.description || '—'}</td>
                <td className="px-4 py-3 text-gray-500">
                  {(role.permissions ?? []).length} permission{(role.permissions ?? []).length > 1 ? 's' : ''}
                </td>
                <td className="px-4 py-3 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className="rounded p-1.5 text-gray-400 enabled:hover:bg-gray-100 enabled:hover:text-brand-600 disabled:cursor-not-allowed disabled:opacity-30"
                      title={role.is_system ? 'Rôle système — non modifiable' : 'Modifier'}
                      disabled={role.is_system}
                      onClick={() => setEditingRole(role)}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 enabled:hover:bg-red-50 enabled:hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-30"
                      title={role.is_system ? 'Rôle système — non supprimable' : 'Supprimer'}
                      disabled={role.is_system}
                      onClick={() => {
                        if (confirm(`Supprimer le rôle "${role.name}" ? Les utilisateurs l'ayant ne seront pas supprimés.`)) {
                          deleteRole.mutate(role.id)
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
            {roles.length} rôle{roles.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {editingRole && (
        <RoleFormModal role={editingRole} onClose={() => setEditingRole(null)} />
      )}
    </div>
  )
}
