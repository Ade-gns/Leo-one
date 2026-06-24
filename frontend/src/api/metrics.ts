import { get } from './client'
import type { ApiResponse } from '@/types/api'
import type { MetricType, MetricQueryResult, LatestMetrics } from '@/types/metric'

export const metricsApi = {
  query: (agentID: string, params: {
    type: MetricType
    from: string
    to:   string
  }) =>
    get<ApiResponse<MetricQueryResult>>(
      `/api/v1/agents/${agentID}/metrics`,
      params as Record<string, string>,
    ),

  latest: (agentID: string) =>
    get<ApiResponse<LatestMetrics>>(`/api/v1/agents/${agentID}/metrics/latest`),
}
