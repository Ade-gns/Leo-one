import { get, post, patch, del } from './client'
import type { ApiResponse } from '@/types/api'
import type { Workspace, CreateWorkspacePayload, UpdateWorkspacePayload } from '@/types/workspace'

const BASE = '/api/v1/workspaces'

export const workspacesApi = {
  list: () =>
    get<ApiResponse<Workspace[]>>(BASE),

  create: (payload: CreateWorkspacePayload) =>
    post<ApiResponse<Workspace>>(BASE, payload),

  update: (workspaceID: string, payload: UpdateWorkspacePayload) =>
    patch<ApiResponse<Workspace>>(`${BASE}/${workspaceID}`, payload),

  delete: (workspaceID: string) =>
    del<void>(`${BASE}/${workspaceID}`),
}
