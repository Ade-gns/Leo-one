import { get, patch } from './client'
import type { ApiResponse } from '@/types/api'
import type { TenantSettings, UpdateTenantPayload } from '@/types/tenant'

const BASE = '/api/v1/tenant'

export const tenantApi = {
  get: () =>
    get<ApiResponse<TenantSettings>>(BASE),

  update: (payload: UpdateTenantPayload) =>
    patch<ApiResponse<TenantSettings>>(BASE, payload),
}
