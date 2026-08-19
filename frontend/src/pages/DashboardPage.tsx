/**
 * DashboardPage.tsx — Tableau de bord principal MSP
 */
import { Monitor, AlertTriangle, CheckCircle, WifiOff, ShieldAlert } from 'lucide-react'
import { useAgents } from '@/hooks/useAgents'
import { usePatchesSummary } from '@/hooks/usePatches'
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
  const { data: patchSummaryResp, isLoading: patchSummaryLoading } = usePatchesSummary()
  const patchSummary = patchSummaryResp?.data

  return (
    <div className="page-shell flex flex-col gap-6">
      <div>
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.14em] text-brand-700">Vue d'ensemble</p>
        <h1 className="text-2xl font-bold tracking-tight text-slate-950">Tableau de bord</h1>
        <p className="mt-1 text-sm text-slate-500">Vue globale de l'infrastructure supervisée</p>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
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
        <StatCard
          label="Patchs critiques en attente"
          value={patchSummaryLoading ? '…' : (patchSummary?.agents_with_critical_pending ?? 0)}
          icon={ShieldAlert}
          iconColor="text-red-500"
          loading={patchSummaryLoading}
          trend={!patchSummaryLoading && patchSummary ? {
            direction: patchSummary.agents_with_critical_pending > 0 ? 'down' : 'neutral',
            value: `${patchSummary.total_pending_patches} patch(s) en attente au total`,
          } : undefined}
        />
      </div>

      {/* Graphiques & Alertes */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">

        {/* Répartition statuts */}
        <div className="surface-card p-5">
          <h2 className="mb-4 font-semibold text-slate-800">Répartition des statuts</h2>
          <AgentStatusChart />
        </div>

        {/* Alertes récentes */}
        <div className="surface-card p-5">
          <h2 className="mb-4 font-semibold text-slate-800">Alertes actives</h2>
          <RecentAlerts />
        </div>
      </div>
    </div>
  )
}
