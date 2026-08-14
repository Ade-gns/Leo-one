/**
 * useRoles.ts — Hooks React Query pour les rôles et permissions
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { rolesApi, type CreateRolePayload, type UpdateRolePayload } from '@/api/roles'

export const roleKeys = {
  all:  ['roles'] as const,
  list: () => [...roleKeys.all, 'list'] as const,
}

export const permissionKeys = {
  all: ['permissions'] as const,
}

/** Liste des rôles du tenant courant (système + personnalisés), avec permissions. */
export function useRoles() {
  return useQuery({
    queryKey: roleKeys.list(),
    queryFn:  () => rolesApi.list(),
    staleTime: 60_000,
  })
}

/** Catalogue complet des permissions atomiques — change rarement. */
export function usePermissions() {
  return useQuery({
    queryKey: permissionKeys.all,
    queryFn:  () => rolesApi.listPermissions(),
    staleTime: 10 * 60_000,
  })
}

/** Mutation : création d'un rôle personnalisé */
export function useCreateRole() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateRolePayload) => rolesApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: roleKeys.all }),
  })
}

/** Mutation : mise à jour d'un rôle personnalisé (name/description/permission_ids) */
export function useUpdateRole() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ roleID, payload }: { roleID: string; payload: UpdateRolePayload }) =>
      rolesApi.update(roleID, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: roleKeys.all }),
  })
}

/** Mutation : suppression d'un rôle personnalisé */
export function useDeleteRole() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (roleID: string) => rolesApi.delete(roleID),
    onSuccess:  () => {
      qc.invalidateQueries({ queryKey: roleKeys.all })
      // Les utilisateurs qui avaient ce rôle sont désassignés côté backend
      // (cascade) — resynchronise leur liste pour refléter le changement.
      qc.invalidateQueries({ queryKey: ['users'] })
    },
  })
}
