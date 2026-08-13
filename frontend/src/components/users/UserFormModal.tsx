/**
 * UserFormModal.tsx — Création ou modification d'un utilisateur
 *
 * Pas de flux d'invitation par email (aucun service de mail dans ce
 * projet) : à la création, l'admin fixe directement le mot de passe
 * initial — à communiquer à l'utilisateur par un canal hors-ligne.
 */
import { useState } from 'react'
import { X, UserPlus, Loader2, Save } from 'lucide-react'
import { useCreateUser, useUpdateUser } from '@/hooks/useUsers'
import { useRoles } from '@/hooks/useRoles'
import type { User } from '@/types/user'

interface UserFormModalProps {
  user?:   User  // présent = édition, absent = création
  onClose: () => void
}

const MIN_PASSWORD_LEN = 8

export function UserFormModal({ user, onClose }: UserFormModalProps) {
  const isEdit = !!user

  const [email, setEmail]       = useState(user?.email ?? '')
  const [fullName, setFullName] = useState(user?.full_name ?? '')
  const [password, setPassword] = useState('')
  const [isActive, setIsActive] = useState(user?.is_active ?? true)
  const [roleIDs, setRoleIDs]   = useState<string[]>(user?.roles?.map(r => r.id) ?? [])
  const [error, setError]       = useState<string | null>(null)

  const { data: rolesData, isLoading: rolesLoading } = useRoles()
  const roles = rolesData?.data ?? []

  const createUser = useCreateUser()
  const updateUser = useUpdateUser()
  const isPending = createUser.isPending || updateUser.isPending

  const toggleRole = (roleID: string) => {
    setRoleIDs(prev => prev.includes(roleID) ? prev.filter(id => id !== roleID) : [...prev, roleID])
  }

  const canSubmit = isEdit
    ? fullName.trim() !== ''
    : email.trim() !== '' && fullName.trim() !== '' && password.length >= MIN_PASSWORD_LEN

  const handleSubmit = () => {
    if (!canSubmit) return
    setError(null)

    if (isEdit) {
      updateUser.mutate(
        { userID: user.id, payload: { full_name: fullName, is_active: isActive, role_ids: roleIDs } },
        { onSuccess: onClose, onError: err => setError(err instanceof Error ? err.message : 'Erreur inconnue') },
      )
    } else {
      createUser.mutate(
        { email, full_name: fullName, password, role_ids: roleIDs },
        { onSuccess: onClose, onError: err => setError(err instanceof Error ? err.message : 'Erreur inconnue') },
      )
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-md flex-col rounded-2xl bg-white shadow-2xl">

        {/* En-tête */}
        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <UserPlus className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              {isEdit ? 'Modifier l’utilisateur' : 'Nouvel utilisateur'}
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">

          {!isEdit && (
            <div>
              <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Email
              </label>
              <input
                type="email"
                value={email}
                onChange={e => setEmail(e.target.value)}
                placeholder="prenom.nom@exemple.com"
                className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />
            </div>
          )}

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom complet
            </label>
            <input
              type="text"
              value={fullName}
              onChange={e => setFullName(e.target.value)}
              placeholder="Prénom Nom"
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          {!isEdit && (
            <div>
              <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Mot de passe initial
              </label>
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder={`${MIN_PASSWORD_LEN} caractères minimum`}
                className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />
              <p className="mt-1.5 text-xs text-gray-400">
                À communiquer à l'utilisateur — aucun email d'invitation n'est envoyé.
              </p>
            </div>
          )}

          {isEdit && (
            <label className="flex items-center gap-2 text-sm text-gray-700">
              <input
                type="checkbox"
                checked={isActive}
                onChange={e => setIsActive(e.target.checked)}
                className="h-4 w-4 rounded border-gray-300 text-brand-600 focus:ring-brand-500"
              />
              Compte actif
            </label>
          )}

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Rôles
            </label>
            {rolesLoading && <p className="text-xs text-gray-400">Chargement…</p>}
            {!rolesLoading && (
              <div className="flex flex-col gap-1.5 rounded-lg border border-gray-200 p-3">
                {roles.map(role => (
                  <label key={role.id} className="flex items-center gap-2 text-sm text-gray-700">
                    <input
                      type="checkbox"
                      checked={roleIDs.includes(role.id)}
                      onChange={() => toggleRole(role.id)}
                      className="h-4 w-4 rounded border-gray-300 text-brand-600 focus:ring-brand-500"
                    />
                    {role.name}
                  </label>
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
