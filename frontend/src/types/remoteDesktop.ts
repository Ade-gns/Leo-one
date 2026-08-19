export type RemoteDesktopMode   = 'view' | 'control'
export type RemoteDesktopStatus = 'pending' | 'active' | 'ended'

/** Réponse de POST .../remote-desktop/{view,control}-sessions */
export interface RemoteDesktopSession {
  session_id:    string
  mode:          RemoteDesktopMode
  status:        RemoteDesktopStatus
  viewer_token:  string
  viewer_ws_url: string
  expires_at:    string
}

/** Réponse de GET .../remote-desktop/sessions/:sessionID */
export interface RemoteDesktopSessionDetail {
  id:                  string
  tenant_id:           string
  agent_id:            string
  user_id?:            string
  mode:                RemoteDesktopMode
  status:              RemoteDesktopStatus
  expires_at:          string
  agent_connected_at?: string
  viewer_connected_at?: string
  ended_at?:           string
  ended_reason?:       string
  created_at:          string
}
