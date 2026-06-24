/**
 * AgentStatusChart.tsx — Donut chart répartition des statuts d'agents
 */
import { PieChart, Pie, Cell, Tooltip, Legend, ResponsiveContainer } from 'recharts'
import { useAgents } from '@/hooks/useAgents'
import type { AgentStatus } from '@/types/agent'

const STATUS_COLORS: Record<AgentStatus, string> = {
  online:       '#22c55e',
  offline:      '#9ca3af',
  maintenance:  '#f59e0b',
  unresponsive: '#ef4444',
}

const STATUS_LABELS: Record<AgentStatus, string> = {
  online:       'En ligne',
  offline:      'Hors ligne',
  maintenance:  'Maintenance',
  unresponsive: 'Inaccessible',
}

export function AgentStatusChart() {
  const { data, isLoading } = useAgents()
  const agents = data?.data ?? []

  if (isLoading) {
    return <div className="h-48 animate-pulse rounded-lg bg-gray-100" />
  }

  const counts = agents.reduce<Partial<Record<AgentStatus, number>>>((acc, a) => {
    acc[a.status] = (acc[a.status] ?? 0) + 1
    return acc
  }, {})

  const chartData = (Object.keys(counts) as AgentStatus[]).map(status => ({
    name:  STATUS_LABELS[status],
    value: counts[status]!,
    color: STATUS_COLORS[status],
  }))

  if (chartData.length === 0) {
    return (
      <div className="flex h-48 items-center justify-center text-sm text-gray-400">
        Aucun agent enregistré
      </div>
    )
  }

  return (
    <ResponsiveContainer width="100%" height={192}>
      <PieChart>
        <Pie
          data={chartData}
          cx="50%"
          cy="50%"
          innerRadius={50}
          outerRadius={75}
          paddingAngle={3}
          dataKey="value"
          isAnimationActive={false}
        >
          {chartData.map((entry, i) => (
            <Cell key={i} fill={entry.color} />
          ))}
        </Pie>
        <Tooltip
          contentStyle={{ borderRadius: 8, border: '1px solid #e5e7eb', fontSize: 12 }}
          formatter={(v: number, name: string) => [`${v} machine${v > 1 ? 's' : ''}`, name]}
        />
        <Legend
          iconType="circle"
          iconSize={8}
          wrapperStyle={{ fontSize: 12, color: '#6b7280' }}
        />
      </PieChart>
    </ResponsiveContainer>
  )
}
