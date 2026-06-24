export type MetricType =
  | 'cpu_percent'
  | 'ram_used_bytes'
  | 'ram_total_bytes'
  | 'disk_used_bytes'
  | 'disk_total_bytes'
  | 'net_bytes_in'
  | 'net_bytes_out'
  | 'process_count'

export type MetricResolution = 'raw' | '1h' | '1d'

export interface MetricPoint {
  time:      string  // ISO 8601
  value:     number
  avg?:      number
  max?:      number
  min?:      number
}

export interface MetricQueryResult {
  data:       MetricPoint[]
  meta: {
    resolution: MetricResolution
    from:       string
    to:         string
  }
}

/** Dernières métriques connues d'un agent (snapshot live) */
export interface LatestMetrics {
  cpu_percent:         number
  ram_used_bytes:      number
  ram_total_bytes:     number
  disk_used_bytes:     number
  disk_total_bytes:    number
  net_bytes_in:        number
  net_bytes_out:       number
  process_count:       number
  ts:                  string  // ISO 8601
}

/** Métriques reçues en temps réel via WebSocket */
export interface LiveMetricsMessage {
  agent_id:   string
  metrics:    LatestMetrics
}
