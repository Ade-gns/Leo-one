/**
 * StatCard.tsx — Carte KPI pour le tableau de bord
 *
 * Affiche une valeur clé avec icône, label, variation optionnelle.
 */
import { type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

type TrendDirection = 'up' | 'down' | 'neutral'

interface StatCardProps {
  label:      string
  value:      string | number
  icon:       LucideIcon
  iconColor?: string       /* ex: "text-green-500" */
  trend?:     {
    direction: TrendDirection
    value:     string      /* ex: "+3 depuis hier" */
  }
  loading?:   boolean
}

export function StatCard({ label, value, icon: Icon, iconColor, trend, loading }: StatCardProps) {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-gray-500">{label}</span>
        <div className={cn('rounded-lg p-2 bg-gray-50', iconColor)}>
          <Icon className={cn('h-5 w-5', iconColor ?? 'text-gray-600')} />
        </div>
      </div>

      {loading ? (
        <div className="h-8 w-20 animate-pulse rounded bg-gray-100" />
      ) : (
        <p className="text-3xl font-bold text-gray-900">{value}</p>
      )}

      {trend && !loading && (
        <p className={cn(
          'text-xs font-medium',
          trend.direction === 'up'      && 'text-green-600',
          trend.direction === 'down'    && 'text-red-600',
          trend.direction === 'neutral' && 'text-gray-400',
        )}>
          {trend.value}
        </p>
      )}
    </div>
  )
}
