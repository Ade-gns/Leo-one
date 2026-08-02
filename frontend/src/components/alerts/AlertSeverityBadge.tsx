/**
 * AlertSeverityBadge.tsx — Badge coloré indiquant la sévérité d'une alerte
 */
import { cn } from '@/lib/utils'
import type { AlertSeverity } from '@/types/alert'

const SEVERITY_CONFIG: Record<AlertSeverity, { label: string; bg: string; text: string }> = {
  info:     { label: 'Info',     bg: 'bg-blue-50',   text: 'text-blue-700'   },
  warning:  { label: 'Warning',  bg: 'bg-yellow-50', text: 'text-yellow-700' },
  critical: { label: 'Critique', bg: 'bg-red-50',    text: 'text-red-700'    },
}

interface AlertSeverityBadgeProps {
  severity: AlertSeverity
}

export function AlertSeverityBadge({ severity }: AlertSeverityBadgeProps) {
  const cfg = SEVERITY_CONFIG[severity]

  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      {cfg.label}
    </span>
  )
}
