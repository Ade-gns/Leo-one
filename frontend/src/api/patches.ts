import { get, post } from './client'
import type { ApiResponse } from '@/types/api'
import type {
  Patch, InstallPatchesPayload, BulkInstallPatchesPayload, PatchesSummary,
} from '@/types/patch'
import type { BulkCommandResult } from '@/types/agent'

const AGENTS_BASE = '/api/v1/agents'

export const patchesApi = {
  list: (agentID: string) =>
    get<ApiResponse<Patch[]>>(`${AGENTS_BASE}/${agentID}/patches`),

  install: (agentID: string, payload: InstallPatchesPayload) =>
    post<ApiResponse<{ command_id: string; status: string; sent: boolean }>>(
      `${AGENTS_BASE}/${agentID}/patches/install`, payload,
    ),

  bulkInstall: (payload: BulkInstallPatchesPayload) =>
    post<ApiResponse<BulkCommandResult[]>>(`${AGENTS_BASE}/bulk-patches/install`, payload),

  summary: () =>
    get<ApiResponse<PatchesSummary>>('/api/v1/patches/summary'),
}
