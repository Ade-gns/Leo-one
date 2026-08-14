/**
 * RoleFormModal.tsx — Création ou modification d'un rôle personnalisé
 *
 * Les rôles système (is_system=true) ne passent jamais par cette modale en
 * édition — RoleTable désactive leur bouton "Modifier" (et le backend
 * rejetterait de toute façon avec 403 SYSTEM_ROLE_IMMUTABLE).
 */
import { useState } from 'react'
import { X, ShieldCheck, Loader2, Save } from 'lucide-react'
import { useCreateRole, useUpdateRole, usePermissions } from '@/hooks/useRoles'
import type { Role } from '@/types/user'

interface RoleFormModalProps {
  role?:   Role  // présent = édition, absent = création
  onClose: () => void
}

export function RoleFormModal({ role, onClose }: RoleFormModalProps) {
  const isEdit = !!role

  const [name, setName]               = useState(role?.name ?? '')
  const [description, setDescription] = useState(role?.description ?? '')
  const [permissionIDs, setPermissionIDs] = useState<string[]>(role?.permissions?.map(p => p.id) ?? [])
  const [error, setError]             = useState<string | null>(null)

  const { data: permsData, isLoading: permsLoading } = usePermissions()
  const permissions = permsData?.data ?? []

  // Regroupe par ressource (agents, alerts, ...) pour un affichage lisible,
  // plutôt qu'une liste plate de dizaines de cases à cocher.
  const grouped = permissions.reduce<Record<string, typeof permissions>>((acc, p) => {
    (acc[p.resource] ??= []).push(p)
    return acc
  }, {})

  const createRole = useCreateRole()
  const updateRole = useUpdateRole()
  const isPending = createRole.isPending || updateRole.isPending

  const togglePermission = (id: string) => {
    setPermissionIDs(prev => prev.includes(id) ? prev.filter(p => p !== id) : [...prev, id])
  }

  const canSubmit = name.trim() !== ''

  const handleSubmit = () => {
    if (!canSubmit) return
    setError(null)

    const payload = { name: name.trim(), description: description.trim() || undefined, permission_ids: permissionIDs }
    const onError = (err: unknown) => setError(err instanceof Error ? err.message : 'Erreur inconnue')

    if (isEdit) {
      updateRole.mutate({ roleID: role.id, payload }, { onSuccess: onClose, onError })
    } else {
      createRole.mutate(payload, { onSuccess: onClose, onError })
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-lg max-h-[85vh] flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <ShieldCheck className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              {isEdit ? 'Modifier le rôle' : 'Nouveau rôle'}
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 overflow-y-auto p-6">
          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="ex : Superviseur"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Description (optionnel)
            </label>
            <input
              type="text"
              value={description}
              onChange={e => setDescription(e.target.value)}
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Permissions
            </label>
            {permsLoading && <p className="text-xs text-gray-400">Chargement…</p>}
            {!permsLoading && (
              <div className="flex flex-col gap-3 rounded-lg border border-gray-200 p-3">
                {Object.entries(grouped).map(([resource, perms]) => (
                  <div key={resource}>
                    <p className="mb-1 text-xs font-semibold capitalize text-gray-500">{resource}</p>
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      {perms.map(p => (
                        <label key={p.id} className="flex items-center gap-1.5 text-sm text-gray-700">
                          <input
                            type="checkbox"
                            checked={permissionIDs.includes(p.id)}
                            onChange={() => togglePermission(p.id)}
                            className="h-4 w-4 rounded border-gray-300 text-brand-600 focus:ring-brand-500"
                          />
                          {p.action}
                        </label>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {error && <p className="text-xs text-red-500">Erreur : {error}</p>}

          <div className="flex justify-end">
            <button
              onClick={handleSubmit}
              disabled={!canSubmit || isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Enregistrement…</>
                : <><Save className="h-4 w-4" />{isEdit ? 'Enregistrer' : 'Créer'}</>
              }
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
