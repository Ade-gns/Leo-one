/**
 * RamChart.tsx — Graphique d'utilisation mémoire (bytes → GB)
 */
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer,
} from 'recharts'
import { format } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useMetricHistory } from '@/hooks/useMetrics'
import { formatBytes } from '@/lib/utils'

interface RamChartProps {
  agentId:    string
  rangeHours: number
  totalBytes?: number
}

export function RamChart({ agentId, rangeHours, totalBytes }: RamChartProps) {
  const { data, isLoading } = useMetricHistory(agentId, 'ram_used_bytes', rangeHours)

  if (isLoading) {
    return <div className="h-48 animate-pulse rounded-lg bg-gray-100" />
  }

  const points = data?.data?.data ?? []
  const domainMax = totalBytes ?? (points.reduce((m, p) => Math.max(m, p.value), 0) || 1)

  return (
    <ResponsiveContainer width="100%" height={192}>
      <AreaChart data={points} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="ramGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%"  stopColor="#8b5cf6" stopOpacity={0.3} />
            <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0}   />
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
          domain={[0, domainMax]}
          tickFormatter={v => formatBytes(v, 0)}
          tick={{ fontSize: 11, fill: '#9ca3af' }}
          tickLine={false}
          axisLine={false}
          width={52}
        />
        <Tooltip
          contentStyle={{ borderRadius: 8, border: '1px solid #e5e7eb', fontSize: 12 }}
          formatter={(v: number) => [formatBytes(v), 'RAM utilisée']}
          labelFormatter={v => format(new Date(v), 'dd/MM/yyyy HH:mm', { locale: fr })}
        />
        <Area
          type="monotone"
          dataKey="value"
          stroke="#8b5cf6"
          strokeWidth={2}
          fill="url(#ramGradient)"
          dot={false}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}
