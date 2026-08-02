import { get, post } from './client'
import type { ApiResponse, PaginationParams } from '@/types/api'
import type { Alert, AlertListFilter } from '@/types/alert'

const BASE = '/api/v1/alerts'

export const alertsApi = {
  list: (filter?: AlertListFilter & PaginationParams) =>
    get<ApiResponse<Alert[]>>(BASE, filter as Record<string, string>),

  acknowledge: (alertID: string) =>
    post<ApiResponse<Alert>>(`${BASE}/${alertID}/acknowledge`),
}
