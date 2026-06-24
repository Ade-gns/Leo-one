/**
 * CpuChart.tsx — Graphique d'utilisation CPU sur une plage temporelle
 */
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer,
} from 'recharts'
import { format } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useMetricHistory } from '@/hooks/useMetrics'
import { formatPercent } from '@/lib/utils'

interface CpuChartProps {
  agentId:    string
  rangeHours: number
}

export function CpuChart({ agentId, rangeHours }: CpuChartProps) {
  const { data, isLoading } = useMetricHistory(agentId, 'cpu_percent', rangeHours)

  if (isLoading) {
    return <div className="h-48 animate-pulse rounded-lg bg-gray-100" />
  }

  const points = data?.data?.data ?? []

  return (
    <ResponsiveContainer width="100%" height={192}>
      <AreaChart data={points} margin={{ top: 4, right: 8, left: -16, bottom: 0 }}>
        <defs>
          <linearGradient id="cpuGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%"  stopColor="#3b82f6" stopOpacity={0.3} />
            <stop offset="95%" stopColor="#3b82f6" stopOpacity={0}   />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis
          dataKey="time"
          tickFormatter={v => format(new Date(v), rangeHours <= 6 ? 'HH:mm' : 'dd/MM HH:mm', { locale: fr })}
          tick={{ fontSize: 11, fill: '#9ca3af' }}
          tickLine={false}
          axisLine={false}
        />
        <YAxis
          domain={[0, 100]}
          tickFormatter={v => `${v}%`}
          tick={{ fontSize: 11, fill: '#9ca3af' }}
          tickLine={false}
          axisLine={false}
        />
        <Tooltip
          contentStyle={{ borderRadius: 8, border: '1px solid #e5e7eb', fontSize: 12 }}
          formatter={(v: number) => [formatPercent(v), 'CPU']}
          labelFormatter={v => format(new Date(v), 'dd/MM/yyyy HH:mm', { locale: fr })}
        />
        <Area
          type="monotone"
          dataKey="value"
          stroke="#3b82f6"
          strokeWidth={2}
          fill="url(#cpuGradient)"
          dot={false}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}
