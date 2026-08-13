export interface Tenant {
  id:         string
  name:       string
  slug:       string
  plan:       string
  max_agents: number
  is_active:  boolean
  created_at: string
  updated_at: string
}

export interface TenantSettings extends Tenant {
  agent_count: number
  plan_limits: { max_agents: number }
}

export interface UpdateTenantPayload {
  name?: string
}
