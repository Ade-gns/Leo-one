/**
 * DiskUsageBar.tsx — Barre de progression pour l'utilisation disque par point de montage
 */
import { cn, formatBytes, formatPercent } from '@/lib/utils'

interface DiskUsageBarProps {
  mountpoint: string
  usedBytes:  number
  totalBytes: number
}

export function DiskUsageBar({ mountpoint, usedBytes, totalBytes }: DiskUsageBarProps) {
  const pct = totalBytes > 0 ? (usedBytes / totalBytes) * 100 : 0

  const barColor =
    pct >= 90 ? 'bg-red-500'    :
    pct >= 75 ? 'bg-yellow-500' :
                'bg-green-500'

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs text-gray-600">
        <span className="font-mono font-medium truncate max-w-[60%]">{mountpoint}</span>
        <span className="text-gray-400">
          {formatBytes(usedBytes)} / {formatBytes(totalBytes)}
        </span>
      </div>
      <div className="relative h-2 rounded-full bg-gray-100 overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-500', barColor)}
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <div className={cn(
        'text-right text-xs font-medium',
        pct >= 90 ? 'text-red-600' : pct >= 75 ? 'text-yellow-600' : 'text-green-600',
      )}>
        {formatPercent(pct)}
      </div>
    </div>
  )
}
