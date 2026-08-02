/**
 * RecentAlerts.tsx — Liste des dernières alertes non acquittées
 */
import { AlertTriangle, AlertCircle, Info, CheckCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useAlerts } from '@/hooks/useAlerts'
import type { AlertSeverity } from '@/types/alert'

const SEVERITY_CONFIG: Record<AlertSeverity, { icon: React.ElementType; color: string }> = {
  critical: { icon: AlertCircle,   color: 'text-red-500'    },
  warning:  { icon: AlertTriangle, color: 'text-yellow-500' },
  info:     { icon: Info,          color: 'text-blue-500'   },
}

export function RecentAlerts() {
  const { data, isLoading } = useAlerts({ status: 'open' })

  const alerts = (data?.data ?? []).slice(0, 5)

  if (isLoading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-12 animate-pulse rounded-lg bg-gray-100" />
        ))}
      </div>
    )
  }

  if (alerts.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-gray-400">
        <CheckCircle className="h-8 w-8 mb-2 text-green-400" />
        <span className="text-sm">Aucune alerte active</span>
      </div>
    )
  }

  return (
    <ul className="space-y-2">
      {alerts.map(alert => {
        const cfg = SEVERITY_CONFIG[alert.severity]
        const Icon = cfg.icon
        return (
          <li
            key={alert.id}
            className="flex items-start gap-3 rounded-lg border border-gray-100 p-3 hover:bg-gray-50"
          >
            <Icon className={`h-5 w-5 mt-0.5 shrink-0 ${cfg.color}`} />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-gray-800 truncate">{alert.title}</p>
              <p className="text-xs text-gray-400 mt-0.5">
                {formatDistanceToNow(new Date(alert.triggered_at), { addSuffix: true, locale: fr })}
              </p>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
