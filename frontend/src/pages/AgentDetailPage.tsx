/**
 * AgentDetailPage.tsx — Vue détaillée d'un agent avec métriques et actions
 */
import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ChevronLeft, Terminal, RefreshCw, AlertCircle } from 'lucide-react'
import { useAgent } from '@/hooks/useAgents'
import { AgentStatusBadge }  from '@/components/agents/AgentStatusBadge'
import { AgentDetailPanel }  from '@/components/agents/AgentDetailPanel'
import { AgentCommandModal } from '@/components/agents/AgentCommandModal'
import { MetricsGrid }       from '@/components/metrics/MetricsGrid'

export default function AgentDetailPage() {
  const { agentId }     = useParams<{ agentId: string }>()
  const navigate        = useNavigate()
  const [showModal, setShowModal] = useState(false)

  const { data, isLoading, isError, refetch } = useAgent(agentId!)

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <div className="h-8 w-48 animate-pulse rounded bg-gray-100" />
        <div className="h-64 animate-pulse rounded-xl bg-gray-100" />
      </div>
    )
  }

  if (isError || !data?.data) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 p-16 text-gray-400">
        <AlertCircle className="h-12 w-12 text-red-300" />
        <p className="font-medium">Machine introuvable</p>
        <button
          onClick={() => navigate('/agents')}
          className="text-sm text-brand-600 hover:underline"
        >
          Retour à la liste
        </button>
      </div>
    )
  }

  const agent = data.data

  return (
    <div className="flex flex-col gap-6 p-6">

      {/* En-tête */}
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <button
            onClick={() => navigate('/agents')}
            className="mb-2 flex items-center gap-1 text-sm text-gray-400 hover:text-gray-600"
          >
            <ChevronLeft className="h-4 w-4" />
            Machines
          </button>
          <div className="flex items-center gap-3">
            <h1 className="text-xl font-bold text-gray-900">{agent.hostname}</h1>
            <AgentStatusBadge status={agent.status} />
          </div>
          <p className="text-sm text-gray-400 mt-0.5 font-mono">{agent.ip_address ?? 'Adresse IP inconnue'}</p>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={() => refetch()}
            className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
          <button
            onClick={() => setShowModal(true)}
            disabled={agent.status !== 'online'}
            className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <Terminal className="h-4 w-4" />
            Exécuter un script
          </button>
        </div>
      </div>

      {/* Contenu en deux colonnes */}
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-3">

        {/* Métriques temps-réel (2/3) */}
        <div className="xl:col-span-2">
          <MetricsGrid agentId={agent.id} />
        </div>

        {/* Informations agent (1/3) */}
        <div className="xl:col-span-1">
          <AgentDetailPanel agent={agent} />
        </div>
      </div>

      {/* Modal console */}
      {showModal && (
        <AgentCommandModal
          agentId={agent.id}
          hostname={agent.hostname}
          onClose={() => setShowModal(false)}
        />
      )}
    </div>
  )
}
