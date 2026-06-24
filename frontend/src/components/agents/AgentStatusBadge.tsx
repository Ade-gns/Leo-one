/**
 * AgentStatusBadge.tsx — Badge coloré indiquant le statut d'un agent
 */
import { cn } from '@/lib/utils'
import type { AgentStatus } from '@/types/agent'

const STATUS_CONFIG: Record<AgentStatus, { label: string; dot: string; bg: string; text: string }> = {
  online:        { label: 'En ligne',      dot: 'bg-green-500',  bg: 'bg-green-50',  text: 'text-green-700' },
  offline:       { label: 'Hors ligne',    dot: 'bg-gray-400',   bg: 'bg-gray-100',  text: 'text-gray-600'  },
  maintenance:   { label: 'Maintenance',   dot: 'bg-yellow-500', bg: 'bg-yellow-50', text: 'text-yellow-700'},
  unresponsive:  { label: 'Inaccessible',  dot: 'bg-red-500',    bg: 'bg-red-50',    text: 'text-red-700'   },
}

interface AgentStatusBadgeProps {
  status: AgentStatus
  pulse?: boolean  /* animation pulse quand online */
}

export function AgentStatusBadge({ status, pulse = true }: AgentStatusBadgeProps) {
  const cfg = STATUS_CONFIG[status]

  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      <span className={cn('h-1.5 w-1.5 rounded-full', cfg.dot, pulse && status === 'online' && 'animate-pulse')} />
      {cfg.label}
    </span>
  )
}
