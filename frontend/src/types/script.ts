export type ScriptInterpreter = 'bash' | 'sh' | 'python' | 'cmd' | 'powershell'

export interface Script {
  id:           string
  name:         string
  description?: string
  interpreter:  ScriptInterpreter
  content:      string
  created_at:   string
  updated_at:   string
}

export interface CreateScriptPayload {
  name:         string
  description?: string
  interpreter:  ScriptInterpreter
  content:      string
}

export type UpdateScriptPayload = Partial<CreateScriptPayload>

export interface ScriptSchedule {
  id:               string
  script_id:        string
  name:             string
  agent_id?:        string
  workspace_id?:    string
  // Exactement l'un des deux : cron_expression pour une récurrence, run_at
  // (ISO 8601) pour une exécution ponctuelle à une date/heure précise —
  // voir ScheduleHandler côté backend.
  cron_expression?: string
  run_at?:          string
  timeout_sec:      number
  enabled:          boolean
  next_run_at:      string
  last_run_at?:     string
  created_at:       string
  updated_at:       string
}

export interface CreateSchedulePayload {
  script_id:        string
  name:             string
  agent_id?:        string
  workspace_id?:    string
  cron_expression?: string
  run_at?:          string
  timeout_sec?:     number
}

export type UpdateSchedulePayload = Partial<Omit<CreateSchedulePayload, 'script_id'>> & {
  enabled?: boolean
}
