import type { MetricType } from './metric'

export type AlertSeverity = 'info' | 'warning' | 'critical'
export type AlertStatus   = 'open' | 'acknowledged' | 'resolved'

export interface Alert {
  id:               string
  tenant_id:        string
  agent_id:         string
  agent_hostname:   string
  rule_id?:         string
  severity:         AlertSeverity
  status:           AlertStatus
  title:            string
  description?:     string
  metric_value?:    number
  triggered_at:     string
  acknowledged_at?: string
  acknowledged_by?: string
  resolved_at?:     string
  created_at:       string
}

export interface AlertListFilter {
  status?:   AlertStatus
  severity?: AlertSeverity
  agent_id?: string
}

export interface AlertRule {
  id:             string
  tenant_id:      string
  workspace_id?:  string
  agent_id?:      string
  name:           string
  description?:   string
  metric_type:    MetricType
  operator:       '>' | '>=' | '<' | '<=' | '='
  threshold:      number
  duration_secs:  number
  severity:       AlertSeverity
  is_active:      boolean
  created_by?:    string
  created_at:     string
  updated_at:     string
}
