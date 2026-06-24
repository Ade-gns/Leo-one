/**
 * useMetrics.ts — Hooks React Query pour les métriques
 */
import { useQuery } from '@tanstack/react-query'
import { subHours, formatISO } from 'date-fns'
import { metricsApi } from '@/api/metrics'
import type { MetricType } from '@/types/metric'

export const metricKeys = {
  latest:  (agentID: string)                              => ['metrics', 'latest', agentID] as const,
  history: (agentID: string, type: MetricType, range: string) =>
    ['metrics', 'history', agentID, type, range] as const,
}

/** Dernières métriques connues (rafraîchi toutes les 30s si pas de WS) */
export function useLatestMetrics(agentID: string) {
  return useQuery({
    queryKey: metricKeys.latest(agentID),
    queryFn:  () => metricsApi.latest(agentID),
    enabled:  !!agentID,
    refetchInterval: 30_000,
    staleTime:        15_000,
  })
}

/** Historique des métriques sur une plage de temps */
export function useMetricHistory(
  agentID:   string,
  type:      MetricType,
  rangeHours: number = 6,
) {
  const to   = new Date()
  const from = subHours(to, rangeHours)

  return useQuery({
    queryKey: metricKeys.history(agentID, type, `${rangeHours}h`),
    queryFn:  () => metricsApi.query(agentID, {
      type,
      from: formatISO(from),
      to:   formatISO(to),
    }),
    enabled:  !!agentID,
    staleTime: 60_000,
    refetchInterval: 60_000,
  })
}
