/**
 * AgentDetailPanel.tsx — Panneau d'info détaillée d'un agent (hardware, logiciels, certificat)
 */
import { useState } from 'react'
import { Cpu, HardDrive, Package, FileText } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useHardwareInventory } from '@/hooks/useAgents'
import { useQuery } from '@tanstack/react-query'
import { agentsApi } from '@/api/agents'
import { formatBytes } from '@/lib/utils'
import { cn } from '@/lib/utils'
import type { Agent } from '@/types/agent'

type Tab = 'hardware' | 'software' | 'info'

interface AgentDetailPanelProps {
  agent: Agent
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between py-2 border-b border-gray-50 last:border-0">
      <span className="text-xs text-gray-400 font-medium w-36 shrink-0">{label}</span>
      <span className="text-xs text-gray-700 font-mono text-right break-all">{value ?? '—'}</span>
    </div>
  )
}

export function AgentDetailPanel({ agent }: AgentDetailPanelProps) {
  const [tab, setTab] = useState<Tab>('hardware')

  const { data: hwResp, isLoading: hwLoading } = useHardwareInventory(agent.id)
  const { data: swResp, isLoading: swLoading } = useQuery({
    queryKey: ['agents', agent.id, 'software'],
    queryFn:  () => agentsApi.getSoftwareInventory(agent.id),
    enabled:  tab === 'software',
  })

  const hw = hwResp?.data
  const sw = swResp?.data ?? []

  const tabs: { key: Tab; label: string; icon: React.ElementType }[] = [
    { key: 'hardware', label: 'Matériel',  icon: HardDrive },
    { key: 'software', label: 'Logiciels', icon: Package },
    { key: 'info',     label: 'Infos',     icon: FileText },
  ]

  return (
    <div className="flex flex-col gap-0 rounded-xl border border-gray-200 bg-white shadow-sm">

      {/* Onglets */}
      <div className="flex border-b border-gray-100">
        {tabs.map(t => {
          const Icon = t.icon
          return (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={cn(
                'flex items-center gap-2 px-5 py-3 text-sm font-medium border-b-2 -mb-px transition-colors',
                tab === t.key
                  ? 'border-brand-600 text-brand-600'
                  : 'border-transparent text-gray-500 hover:text-gray-700',
              )}
            >
              <Icon className="h-4 w-4" />
              {t.label}
            </button>
          )
        })}
      </div>

      <div className="p-5">

        {/* Hardware */}
        {tab === 'hardware' && (
          hwLoading
            ? <div className="space-y-2">{Array.from({ length: 6 }).map((_, i) => <div key={i} className="h-6 animate-pulse rounded bg-gray-100" />)}</div>
            : hw
              ? (
                <div className="space-y-4">
                  <div>
                    <div className="flex items-center gap-2 mb-2 text-xs font-semibold uppercase text-gray-400 tracking-wider">
                      <Cpu className="h-3.5 w-3.5" /> Processeur
                    </div>
                    <InfoRow label="Modèle"      value={hw.cpu_model} />
                    <InfoRow label="Cœurs"       value={hw.cpu_cores} />
                    <InfoRow label="Threads"     value={hw.cpu_threads} />
                  </div>
                  <div>
                    <div className="flex items-center gap-2 mb-2 text-xs font-semibold uppercase text-gray-400 tracking-wider">
                      <HardDrive className="h-3.5 w-3.5" /> Mémoire
                    </div>
                    <InfoRow label="RAM totale"  value={hw.ram_total_bytes ? formatBytes(hw.ram_total_bytes) : undefined} />
                    <InfoRow label="Carte mère"  value={hw.motherboard} />
                    <InfoRow label="BIOS"        value={hw.bios_version} />
                    <InfoRow label="N° série"    value={hw.serial_number} />
                  </div>
                </div>
              )
              : <p className="text-sm text-gray-400">Inventaire non disponible</p>
        )}

        {/* Software */}
        {tab === 'software' && (
          swLoading
            ? <div className="space-y-2">{Array.from({ length: 8 }).map((_, i) => <div key={i} className="h-6 animate-pulse rounded bg-gray-100" />)}</div>
            : (
              <div className="overflow-auto max-h-80">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-gray-100">
                      <th className="pb-2 text-left font-semibold text-gray-500">Nom</th>
                      <th className="pb-2 text-left font-semibold text-gray-500">Version</th>
                      <th className="pb-2 text-left font-semibold text-gray-500">Éditeur</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sw.map((item, i) => (
                      <tr key={i} className="border-b border-gray-50">
                        <td className="py-1.5 font-medium text-gray-800">{item.name}</td>
                        <td className="py-1.5 text-gray-500 font-mono">{item.version ?? '—'}</td>
                        <td className="py-1.5 text-gray-400">{item.publisher ?? '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )
        )}

        {/* Infos agent */}
        {tab === 'info' && (
          <div>
            <InfoRow label="ID agent"       value={agent.id} />
            <InfoRow label="Tenant ID"      value={agent.tenant_id} />
            <InfoRow label="Hostname"       value={agent.hostname} />
            <InfoRow label="OS"             value={`${agent.os} ${agent.os_version}`} />
            <InfoRow label="Architecture"   value={agent.arch} />
            <InfoRow label="Version agent"  value={agent.agent_version} />
            <InfoRow label="Adresse IP"     value={agent.ip_address} />
            <InfoRow label="Enregistré le"  value={agent.enrolled_at ? formatDistanceToNow(new Date(agent.enrolled_at), { addSuffix: true, locale: fr }) : undefined} />
            <InfoRow label="Dernière vue"   value={agent.last_seen_at ? formatDistanceToNow(new Date(agent.last_seen_at), { addSuffix: true, locale: fr }) : undefined} />
          </div>
        )}
      </div>
    </div>
  )
}
