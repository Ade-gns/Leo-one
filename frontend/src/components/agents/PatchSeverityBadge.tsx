/**
 * PatchSeverityBadge.tsx — Badge coloré indiquant la sévérité d'un patch
 */
import { cn } from '@/lib/utils'
import type { PatchSeverity } from '@/types/patch'

const SEVERITY_CONFIG: Record<PatchSeverity, { label: string; bg: string; text: string }> = {
  optional:  { label: 'Optionnel',  bg: 'bg-gray-50',   text: 'text-gray-600'   },
  important: { label: 'Important',  bg: 'bg-yellow-50', text: 'text-yellow-700' },
  critical:  { label: 'Critique',   bg: 'bg-red-50',    text: 'text-red-700'    },
}

interface PatchSeverityBadgeProps {
  severity: PatchSeverity
}

export function PatchSeverityBadge({ severity }: PatchSeverityBadgeProps) {
  const cfg = SEVERITY_CONFIG[severity]

  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      {cfg.label}
    </span>
  )
}
