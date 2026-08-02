import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alertsApi } from '@/api/alerts'
import type { AlertListFilter } from '@/types/alert'

export const alertKeys = {
  all:  ['alerts'] as const,
  list: (filter?: AlertListFilter) => [...alertKeys.all, 'list', filter] as const,
}

/** Liste des alertes avec filtres optionnels */
export function useAlerts(filter?: AlertListFilter) {
  return useQuery({
    queryKey: alertKeys.list(filter),
    queryFn:  () => alertsApi.list(filter),
    staleTime: 15_000,
  })
}

/** Mutation : acquittement d'une alerte */
export function useAcknowledgeAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (alertID: string) => alertsApi.acknowledge(alertID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: alertKeys.all }),
  })
}
