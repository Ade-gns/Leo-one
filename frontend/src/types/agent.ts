export type AgentStatus = 'online' | 'offline' | 'maintenance' | 'unresponsive'
export type AgentOS     = 'windows' | 'linux' | 'macos'

export interface Agent {
  id:            string
  tenant_id:     string
  workspace_id?: string
  hostname:      string
  fqdn?:         string
  os:            AgentOS
  os_version:    string
  arch:          string
  hardware_id:   string
  ip_address?:   string
  agent_version: string
  status:        AgentStatus
  last_seen_at?: string  // ISO 8601
  enrolled_at:   string
  created_at:    string
  updated_at:    string
}

export interface AgentListFilter {
  workspace_id?: string
  status?:       AgentStatus
  search?:       string
}

export interface Command {
  id:           string
  agent_id:     string
  type:         'exec_script' | 'install_pkg' | 'reboot' | 'ping'
  payload:      Record<string, unknown>
  status:       'pending' | 'running' | 'success' | 'failed' | 'timeout'
  stdout?:      string
  stderr?:      string
  exit_code?:   number
  created_by?:  string
  sent_at?:     string
  completed_at?: string
  created_at:   string
}

export interface ExecScriptPayload {
  interpreter:  'powershell' | 'bash' | 'cmd' | 'python'
  script:       string
  timeout_secs: number
}

export interface HardwareInventory {
  id:               string
  agent_id:         string
  cpu_model?:       string
  cpu_cores?:       number
  cpu_threads?:     number
  ram_total_bytes?: number
  bios_version?:    string
  bios_vendor?:     string
  motherboard?:     string
  serial_number?:   string
  collected_at:     string
}

export interface SoftwareItem {
  id:            string
  name:          string
  version?:      string
  publisher?:    string
  install_date?: string
  install_path?: string
  collected_at:  string
}
