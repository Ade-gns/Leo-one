/**
 * AlertStatusBadge.tsx — Badge coloré indiquant le statut d'une alerte
 */
import { cn } from '@/lib/utils'
import type { AlertStatus } from '@/types/alert'

const STATUS_CONFIG: Record<AlertStatus, { label: string; bg: string; text: string }> = {
  open:         { label: 'Ouverte',   bg: 'bg-red-50',    text: 'text-red-700'    },
  acknowledged: { label: 'Acquittée', bg: 'bg-yellow-50', text: 'text-yellow-700' },
  resolved:     { label: 'Résolue',   bg: 'bg-green-50',  text: 'text-green-700'  },
}

interface AlertStatusBadgeProps {
  status: AlertStatus
}

export function AlertStatusBadge({ status }: AlertStatusBadgeProps) {
  const cfg = STATUS_CONFIG[status]

  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      {cfg.label}
    </span>
  )
}
