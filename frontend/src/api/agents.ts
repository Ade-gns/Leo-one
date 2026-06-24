import { get, post, patch, del } from './client'
import type { ApiResponse, PaginationParams } from '@/types/api'
import type {
  Agent, AgentListFilter, Command,
  ExecScriptPayload, HardwareInventory, SoftwareItem,
} from '@/types/agent'

const BASE = '/api/v1/agents'

export const agentsApi = {
  list: (filter?: AgentListFilter & PaginationParams) =>
    get<ApiResponse<Agent[]>>(BASE, filter as Record<string, string>),

  get: (agentID: string) =>
    get<ApiResponse<Agent>>(`${BASE}/${agentID}`),

  update: (agentID: string, data: Partial<Pick<Agent, 'workspace_id' | 'hostname'>>) =>
    patch<ApiResponse<Agent>>(`${BASE}/${agentID}`, data),

  delete: (agentID: string) =>
    del<void>(`${BASE}/${agentID}`),

  execScript: (agentID: string, payload: ExecScriptPayload) =>
    post<ApiResponse<Command>>(`${BASE}/${agentID}/commands`, {
      type: 'exec_script',
      payload,
    }),

  listCommands: (agentID: string, params?: PaginationParams) =>
    get<ApiResponse<Command[]>>(`${BASE}/${agentID}/commands`, params as Record<string, string>),

  getCommand: (agentID: string, commandID: string) =>
    get<ApiResponse<Command>>(`${BASE}/${agentID}/commands/${commandID}`),

  getHardwareInventory: (agentID: string) =>
    get<ApiResponse<HardwareInventory>>(`${BASE}/${agentID}/inventory/hardware`),

  getSoftwareInventory: (agentID: string, params?: { search?: string } & PaginationParams) =>
    get<ApiResponse<SoftwareItem[]>>(
      `${BASE}/${agentID}/inventory/software`,
      params as Record<string, string>,
    ),
}
