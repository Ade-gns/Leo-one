/**
 * MetricsGrid.tsx — Grille de métriques temps-réel pour un agent (CPU + RAM + Disk)
 */
import { useState } from 'react'
import { Cpu, MemoryStick, HardDrive } from 'lucide-react'
import { useLatestMetrics } from '@/hooks/useMetrics'
import { CpuChart }      from './CpuChart'
import { RamChart }      from './RamChart'
import { DiskUsageBar }  from './DiskUsageBar'
import { formatPercent, formatBytes } from '@/lib/utils'
import { cn } from '@/lib/utils'

const RANGE_OPTIONS = [
  { label: '1h',  hours: 1   },
  { label: '6h',  hours: 6   },
  { label: '24h', hours: 24  },
  { label: '7j',  hours: 168 },
]

interface MetricsGridProps {
  agentId: string
}

export function MetricsGrid({ agentId }: MetricsGridProps) {
  const [rangeHours, setRangeHours] = useState(6)
  const { data: liveResp, isLoading } = useLatestMetrics(agentId)

  const live = liveResp?.data

  const cpuPct       = live?.cpu_percent ?? 0
  const ramUsed      = live?.ram_used_bytes ?? 0
  const ramTotal     = live?.ram_total_bytes ?? 1
  const ramPct       = ramTotal > 0 ? (ramUsed / ramTotal) * 100 : 0
  const diskUsed     = live?.disk_used_bytes ?? 0
  const diskTotal    = live?.disk_total_bytes ?? 1

  return (
    <div className="flex flex-col gap-6">

      {/* Sélecteur de plage */}
      <div className="flex items-center gap-2">
        <span className="text-sm text-gray-500 mr-1">Plage :</span>
        {RANGE_OPTIONS.map(opt => (
          <button
            key={opt.hours}
            onClick={() => setRangeHours(opt.hours)}
            className={cn(
              'rounded-md px-3 py-1 text-xs font-medium transition-colors',
              rangeHours === opt.hours
                ? 'bg-brand-900 text-white'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
            )}
          >
            {opt.label}
          </button>
        ))}
      </div>

      {/* CPU */}
      <div className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Cpu className="h-5 w-5 text-blue-500" />
            <h3 className="font-semibold text-gray-800">Processeur</h3>
          </div>
          {isLoading
            ? <div className="h-6 w-16 animate-pulse rounded bg-gray-100" />
            : <span className="text-2xl font-bold text-blue-600">{formatPercent(cpuPct)}</span>
          }
        </div>
        <CpuChart agentId={agentId} rangeHours={rangeHours} />
      </div>

      {/* RAM */}
      <div className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
        <div className="mb-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MemoryStick className="h-5 w-5 text-purple-500" />
            <h3 className="font-semibold text-gray-800">Mémoire</h3>
          </div>
          {isLoading
            ? <div className="h-6 w-32 animate-pulse rounded bg-gray-100" />
            : (
              <span className="text-sm text-gray-500">
                <span className="text-xl font-bold text-purple-600">{formatPercent(ramPct)}</span>
                {' '}— {formatBytes(ramUsed)} / {formatBytes(ramTotal)}
              </span>
            )
          }
        </div>
        <RamChart agentId={agentId} rangeHours={rangeHours} totalBytes={ramTotal} />
      </div>

      {/* Disque (total agrégé) */}
      {diskTotal > 1 && (
        <div className="rounded-xl border border-gray-200 bg-white p-5 shadow-sm">
          <div className="mb-4 flex items-center gap-2">
            <HardDrive className="h-5 w-5 text-orange-500" />
            <h3 className="font-semibold text-gray-800">Stockage</h3>
          </div>
          <DiskUsageBar
            mountpoint="Total"
            usedBytes={diskUsed}
            totalBytes={diskTotal}
          />
        </div>
      )}
    </div>
  )
}
