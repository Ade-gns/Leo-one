export interface User {
  id:             string
  tenant_id:      string
  email:          string
  full_name:      string
  is_active:      boolean
  mfa_enabled:    boolean
  last_login_at?: string
  created_at:     string
  updated_at:     string
  roles?:         Role[]
}

export interface Role {
  id:          string
  name:        string
  description?: string
  is_system:   boolean
  permissions?: Permission[]
}

export interface Permission {
  id:       string
  resource: string
  action:   string
}

/** Données de la session utilisateur connecté (stockées dans le store Zustand) */
export interface AuthSession {
  user:         User
  access_token: string
  expires_at:   number  // epoch ms
}
