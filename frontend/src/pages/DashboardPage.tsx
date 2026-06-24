/**
 * DashboardPage.tsx — Tableau de bord principal MSP
 */
import { Monitor, AlertTriangle, CheckCircle, WifiOff } from 'lucide-react'
import { useAgents } from '@/hooks/useAgents'
import { StatCard }         from '@/components/dashboard/StatCard'
import { AgentStatusChart } from '@/components/dashboard/AgentStatusChart'
import { RecentAlerts }     from '@/components/dashboard/RecentAlerts'
import type { AgentStatus } from '@/types/agent'

function countByStatus(agents: { status: AgentStatus }[], status: AgentStatus) {
  return agents.filter(a => a.status === status).length
}

export default function DashboardPage() {
  const { data, isLoading } = useAgents()
  const agents = data?.data ?? []

  return (
    <div className="flex flex-col gap-6 p-6">
      <div>
        <h1 className="text-xl font-bold text-gray-900">Tableau de bord</h1>
        <p className="text-sm text-gray-500 mt-0.5">Vue globale de l'infrastructure supervisée</p>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard
          label="Machines totales"
          value={isLoading ? '…' : agents.length}
          icon={Monitor}
          iconColor="text-blue-500"
          loading={isLoading}
        />
        <StatCard
          label="En ligne"
          value={isLoading ? '…' : countByStatus(agents, 'online')}
          icon={CheckCircle}
          iconColor="text-green-500"
          loading={isLoading}
          trend={!isLoading && agents.length > 0 ? {
            direction: 'neutral',
            value: `${Math.round((countByStatus(agents, 'online') / agents.length) * 100)}% disponibilité`,
          } : undefined}
        />
        <StatCard
          label="Hors ligne"
          value={isLoading ? '…' : countByStatus(agents, 'offline')}
          icon={WifiOff}
          iconColor="text-gray-400"
          loading={isLoading}
        />
        <StatCard
          label="Inaccessibles"
          value={isLoading ? '…' : countByStatus(agents, 'unresponsive')}
          icon={AlertTriangle}
          iconColor="text-red-500"
          loading={isLoading}
        />
      </div>

      {/* Graphiques & Alertes */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">

        {/* Répartition statuts */}
        <div className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
          <h2 className="mb-4 font-semibold text-gray-800">Répartition des statuts</h2>
          <AgentStatusChart />
        </div>

        {/* Alertes récentes */}
        <div className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
          <h2 className="mb-4 font-semibold text-gray-800">Alertes actives</h2>
          <RecentAlerts />
        </div>
      </div>
    </div>
  )
}
