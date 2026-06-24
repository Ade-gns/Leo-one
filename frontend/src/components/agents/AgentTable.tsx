/**
 * AgentTable.tsx — Table paginée des agents avec filtres et actions rapides
 */
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Monitor, Terminal, Trash2, RefreshCw } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useAgents, useDeleteAgent } from '@/hooks/useAgents'
import { AgentStatusBadge } from './AgentStatusBadge'
import type { AgentStatus, AgentOS } from '@/types/agent'

const OS_ICONS: Record<AgentOS, string> = {
  windows: '🪟',
  linux:   '🐧',
  macos:   '🍎',
}

export function AgentTable() {
  const navigate = useNavigate()
  const [statusFilter, setStatusFilter] = useState<AgentStatus | ''>('')
  const [search, setSearch]             = useState('')

  const { data, isLoading, refetch } = useAgents(
    statusFilter ? { status: statusFilter } : undefined,
  )
  const deleteAgent = useDeleteAgent()

  const agents = data?.data ?? []
  const filtered = search
    ? agents.filter(a =>
        a.hostname.toLowerCase().includes(search.toLowerCase()) ||
        (a.ip_address ?? '').includes(search),
      )
    : agents

  return (
    <div className="flex flex-col gap-4">

      {/* Barre de filtre */}
      <div className="flex items-center gap-3 flex-wrap">
        <input
          type="text"
          placeholder="Rechercher par nom ou IP…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500 w-64"
        />

        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value as AgentStatus | '')}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
        >
          <option value="">Tous les statuts</option>
          <option value="online">En ligne</option>
          <option value="offline">Hors ligne</option>
          <option value="maintenance">Maintenance</option>
          <option value="unresponsive">Inaccessible</option>
        </select>

        <button
          onClick={() => refetch()}
          className="ml-auto flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
        >
          <RefreshCw className="h-4 w-4" />
          Actualiser
        </button>
      </div>

      {/* Table */}
      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Machine</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">OS</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Adresse IP</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Statut</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Dernière activité</th>
              <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              Array.from({ length: 5 }).map((_, i) => (
                <tr key={i} className="border-b border-gray-50">
                  {Array.from({ length: 6 }).map((_, j) => (
                    <td key={j} className="px-4 py-3">
                      <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                    </td>
                  ))}
                </tr>
              ))
            )}

            {!isLoading && filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                  <Monitor className="mx-auto h-8 w-8 mb-2 opacity-40" />
                  Aucune machine trouvée
                </td>
              </tr>
            )}

            {!isLoading && filtered.map(agent => (
              <tr
                key={agent.id}
                className="border-b border-gray-50 hover:bg-gray-50 cursor-pointer"
                onClick={() => navigate(`/agents/${agent.id}`)}
              >
                <td className="px-4 py-3 font-medium text-gray-900">{agent.hostname}</td>
                <td className="px-4 py-3 text-gray-500">
                  <span title={`${agent.os} ${agent.os_version}`}>
                    {OS_ICONS[agent.os]} {agent.os_version}
                  </span>
                </td>
                <td className="px-4 py-3 font-mono text-gray-500">{agent.ip_address ?? '—'}</td>
                <td className="px-4 py-3">
                  <AgentStatusBadge status={agent.status} />
                </td>
                <td className="px-4 py-3 text-gray-400 text-xs">
                  {agent.last_seen_at
                    ? formatDistanceToNow(new Date(agent.last_seen_at), { addSuffix: true, locale: fr })
                    : '—'}
                </td>
                <td className="px-4 py-3 text-right" onClick={e => e.stopPropagation()}>
                  <div className="flex items-center justify-end gap-1">
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-gray-100 hover:text-brand-600"
                      title="Exécuter un script"
                      onClick={() => navigate(`/agents/${agent.id}?tab=console`)}
                    >
                      <Terminal className="h-4 w-4" />
                    </button>
                    <button
                      className="rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                      title="Supprimer"
                      onClick={() => {
                        if (confirm(`Supprimer ${agent.hostname} ?`)) {
                          deleteAgent.mutate(agent.id)
                        }
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {/* Footer avec compteur */}
        {!isLoading && (
          <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
            {filtered.length} machine{filtered.length > 1 ? 's' : ''}
            {filtered.length !== agents.length && ` sur ${agents.length}`}
          </div>
        )}
      </div>
    </div>
  )
}
