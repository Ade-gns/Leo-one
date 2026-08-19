import { post, get, del } from './client'
import type { ApiResponse } from '@/types/api'
import type { RemoteDesktopSession, RemoteDesktopSessionDetail } from '@/types/remoteDesktop'

const AGENTS_BASE = '/api/v1/agents'

export const remoteDesktopApi = {
  createViewSession: (agentID: string) =>
    post<ApiResponse<RemoteDesktopSession>>(`${AGENTS_BASE}/${agentID}/remote-desktop/view-sessions`),

  createControlSession: (agentID: string) =>
    post<ApiResponse<RemoteDesktopSession>>(`${AGENTS_BASE}/${agentID}/remote-desktop/control-sessions`),

  getSession: (agentID: string, sessionID: string) =>
    get<ApiResponse<RemoteDesktopSessionDetail>>(`${AGENTS_BASE}/${agentID}/remote-desktop/sessions/${sessionID}`),

  stopSession: (agentID: string, sessionID: string) =>
    del<void>(`${AGENTS_BASE}/${agentID}/remote-desktop/sessions/${sessionID}`),
}
