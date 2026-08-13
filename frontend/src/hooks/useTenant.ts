/**
 * useTenant.ts — Hooks React Query pour les paramètres du tenant courant
 */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { tenantApi } from '@/api/tenant'
import type { UpdateTenantPayload } from '@/types/tenant'

export const tenantKeys = {
  all: ['tenant'] as const,
}

/** Paramètres du tenant courant (nom, plan, quota d'agents, agents utilisés) */
export function useTenant() {
  return useQuery({
    queryKey: tenantKeys.all,
    queryFn:  () => tenantApi.get(),
    staleTime: 60_000,
  })
}

/** Mutation : modification du nom du tenant */
export function useUpdateTenant() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: UpdateTenantPayload) => tenantApi.update(payload),
    onSuccess:  () => qc.invalidateQueries({ queryKey: tenantKeys.all }),
  })
}
