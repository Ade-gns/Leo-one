/**
 * useRoles.ts — Hooks React Query pour les rôles (lecture seule)
 */
import { useQuery } from '@tanstack/react-query'
import { rolesApi } from '@/api/roles'

export const roleKeys = {
  all:  ['roles'] as const,
  list: () => [...roleKeys.all, 'list'] as const,
}

/** Liste des rôles du tenant courant — change rarement (rôles système fixes). */
export function useRoles() {
  return useQuery({
    queryKey: roleKeys.list(),
    queryFn:  () => rolesApi.list(),
    staleTime: 5 * 60_000,
  })
}
