/**
 * AgentDetailPanel.tsx — Panneau d'info détaillée d'un agent (hardware, logiciels, certificat)
 */
import { useState } from 'react'
import { Cpu, HardDrive, Package, FileText, ShieldAlert, Loader2, Download } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useHardwareInventory, useSoftwareInventory } from '@/hooks/useAgents'
import { usePatches, useInstallPatches } from '@/hooks/usePatches'
import { PatchSeverityBadge } from '@/components/agents/PatchSeverityBadge'
import { formatBytes } from '@/lib/utils'
import { cn } from '@/lib/utils'
import type { Agent } from '@/types/agent'

type Tab = 'hardware' | 'software' | 'patches' | 'info'

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
  const [selectedPatches, setSelectedPatches] = useState<Set<string>>(new Set())

  const { data: hwResp, isLoading: hwLoading } = useHardwareInventory(agent.id)
  const { data: swResp, isLoading: swLoading } = useSoftwareInventory(agent.id, { enabled: tab === 'software' })
  const { data: patchResp, isLoading: patchLoading } = usePatches(agent.id)
  const installPatches = useInstallPatches(agent.id)

  const hw = hwResp?.data
  const sw = swResp?.data ?? []
  const patches = (patchResp?.data ?? []).filter(p => p.status === 'available')

  const togglePatch = (id: string) => {
    setSelectedPatches(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleInstallSelection = () => {
    if (selectedPatches.size === 0) return
    installPatches.mutate(
      { patch_ids: Array.from(selectedPatches) },
      { onSuccess: () => setSelectedPatches(new Set()) },
    )
  }

  const tabs: { key: Tab; label: string; icon: React.ElementType }[] = [
    { key: 'hardware', label: 'Matériel',  icon: HardDrive },
    { key: 'software', label: 'Logiciels', icon: Package },
    { key: 'patches',  label: 'Patchs',    icon: ShieldAlert },
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
                    <InfoRow label="Disques"     value={hw.disk_count} />
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

        {/* Patchs */}
        {tab === 'patches' && (
          <div className="flex flex-col gap-3">
            {patchLoading ? (
              <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => <div key={i} className="h-8 animate-pulse rounded bg-gray-100" />)}</div>
            ) : patches.length === 0 ? (
              <p className="text-sm text-gray-400">Aucun patch en attente</p>
            ) : (
              <>
                <div className="max-h-72 overflow-auto rounded-lg border border-gray-100">
                  {patches.map(p => (
                    <label
                      key={p.id}
                      className="flex cursor-pointer items-start gap-2 border-b border-gray-50 px-3 py-2 text-xs last:border-b-0 hover:bg-gray-50"
                    >
                      <input
                        type="checkbox"
                        checked={selectedPatches.has(p.native_id)}
                        onChange={() => togglePatch(p.native_id)}
                        className="mt-0.5 h-3.5 w-3.5 rounded border-gray-300 text-brand-600 focus:ring-brand-500"
                      />
                      <div className="flex-1 min-w-0">
                        <p className="truncate font-medium text-gray-800">{p.title}</p>
                        {p.size_bytes ? (
                          <p className="text-gray-400">{formatBytes(p.size_bytes)}</p>
                        ) : null}
                      </div>
                      <PatchSeverityBadge severity={p.severity} />
                    </label>
                  ))}
                </div>

                {installPatches.isError && (
                  <p className="text-xs text-red-500">
                    Erreur : {installPatches.error instanceof Error ? installPatches.error.message : 'Erreur inconnue'}
                  </p>
                )}

                <button
                  onClick={handleInstallSelection}
                  disabled={selectedPatches.size === 0 || installPatches.isPending}
                  className="flex items-center justify-center gap-2 rounded-lg bg-brand-900 px-4 py-2 text-xs font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {installPatches.isPending
                    ? <><Loader2 className="h-3.5 w-3.5 animate-spin" />Installation…</>
                    : <><Download className="h-3.5 w-3.5" />Installer la sélection ({selectedPatches.size})</>
                  }
                </button>
              </>
            )}
          </div>
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
