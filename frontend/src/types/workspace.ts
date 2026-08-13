export interface Workspace {
  id:           string
  tenant_id:    string
  name:         string
  description?: string
  created_at:   string
  updated_at:   string
}

export interface CreateWorkspacePayload {
  name:         string
  description?: string
}

export interface UpdateWorkspacePayload {
  name?:        string
  description?: string
}
