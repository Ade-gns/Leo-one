/**
 * AlertTable.tsx — Table des alertes avec filtres et acquittement
 */
import { useState } from 'react'
import { Bell, Check } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useAlerts, useAcknowledgeAlert } from '@/hooks/useAlerts'
import { AlertSeverityBadge } from './AlertSeverityBadge'
import { AlertStatusBadge } from './AlertStatusBadge'
import type { AlertStatus, AlertSeverity } from '@/types/alert'

export function AlertTable() {
  const [statusFilter, setStatusFilter]     = useState<AlertStatus | ''>('')
  const [severityFilter, setSeverityFilter] = useState<AlertSeverity | ''>('')

  const { data, isLoading, refetch } = useAlerts({
    ...(statusFilter   ? { status: statusFilter }     : {}),
    ...(severityFilter ? { severity: severityFilter } : {}),
  })
  const acknowledge = useAcknowledgeAlert()

  const alerts = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">

      <div className="flex items-center gap-3 flex-wrap">
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value as AlertStatus | '')}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
        >
          <option value="">Tous les statuts</option>
          <option value="open">Ouverte</option>
          <option value="acknowledged">Acquittée</option>
          <option value="resolved">Résolue</option>
        </select>

        <select
          value={severityFilter}
          onChange={e => setSeverityFilter(e.target.value as AlertSeverity | '')}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
        >
          <option value="">Toutes les sévérités</option>
          <option value="info">Info</option>
          <option value="warning">Warning</option>
          <option value="critical">Critique</option>
        </select>

        <button
          onClick={() => refetch()}
          className="ml-auto flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
        >
          Actualiser
        </button>
      </div>

      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Sévérité</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Titre</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Machine</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Statut</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Déclenchée</th>
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

            {!isLoading && alerts.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                  <Bell className="mx-auto h-8 w-8 mb-2 opacity-40" />
                  Aucune alerte
                </td>
              </tr>
            )}

            {!isLoading && alerts.map(alert => (
              <tr key={alert.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3"><AlertSeverityBadge severity={alert.severity} /></td>
                <td className="px-4 py-3 font-medium text-gray-900">{alert.title}</td>
                <td className="px-4 py-3 text-gray-500">{alert.agent_hostname}</td>
                <td className="px-4 py-3"><AlertStatusBadge status={alert.status} /></td>
                <td className="px-4 py-3 text-gray-400 text-xs">
                  {formatDistanceToNow(new Date(alert.triggered_at), { addSuffix: true, locale: fr })}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    disabled={alert.status !== 'open' || acknowledge.isPending}
                    onClick={() => acknowledge.mutate(alert.id)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    <Check className="h-3.5 w-3.5" />
                    Acquitter
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!isLoading && (
          <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
            {alerts.length} alerte{alerts.length > 1 ? 's' : ''}
          </div>
        )}
      </div>
    </div>
  )
}
