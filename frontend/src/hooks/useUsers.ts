/**
 * useUsers.ts — Hooks React Query pour les utilisateurs
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { usersApi, type CreateUserPayload, type UpdateUserPayload } from '@/api/users'

export const userKeys = {
  all:    ['users'] as const,
  list:   () => [...userKeys.all, 'list'] as const,
  detail: (id: string) => [...userKeys.all, 'detail', id] as const,
}

/** Liste des utilisateurs du tenant courant */
export function useUsers() {
  return useQuery({
    queryKey: userKeys.list(),
    queryFn:  () => usersApi.list(),
    staleTime: 30_000,
  })
}

/** Mutation : création d'un utilisateur */
export function useCreateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateUserPayload) => usersApi.create(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: userKeys.all }),
  })
}

/** Mutation : mise à jour partielle d'un utilisateur (full_name/is_active/role_ids) */
export function useUpdateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ userID, payload }: { userID: string; payload: UpdateUserPayload }) =>
      usersApi.update(userID, payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: userKeys.all }),
  })
}

/** Mutation : suppression d'un utilisateur */
export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (userID: string) => usersApi.delete(userID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: userKeys.all }),
  })
}
