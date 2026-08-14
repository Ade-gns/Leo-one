/**
 * ScriptsPage.tsx — Bibliothèque de scripts réutilisables et planifications
 * récurrentes
 */
import { useState } from 'react'
import { FileCode, CalendarClock, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ScriptTable } from '@/components/scripts/ScriptTable'
import { ScriptFormModal } from '@/components/scripts/ScriptFormModal'
import { ScheduleTable } from '@/components/scripts/ScheduleTable'
import { ScheduleFormModal } from '@/components/scripts/ScheduleFormModal'

type Tab = 'library' | 'schedules'

export default function ScriptsPage() {
  const [tab, setTab] = useState<Tab>('library')
  const [showCreateModal, setShowCreateModal] = useState(false)

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <FileCode className="h-6 w-6 text-brand-600" />
          <div>
            <h1 className="text-xl font-bold text-gray-900">Scripts</h1>
            <p className="text-sm text-gray-500 mt-0.5">Bibliothèque de scripts et planifications récurrentes</p>
          </div>
        </div>

        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-700"
        >
          <Plus className="h-4 w-4" />
          {tab === 'library' ? 'Nouveau script' : 'Nouvelle planification'}
        </button>
      </div>

      <div className="flex gap-1 border-b border-gray-200">
        <button
          onClick={() => setTab('library')}
          className={cn(
            'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
            tab === 'library'
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-gray-500 hover:text-gray-700',
          )}
        >
          <FileCode className="h-4 w-4" />
          Bibliothèque
        </button>
        <button
          onClick={() => setTab('schedules')}
          className={cn(
            'flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors',
            tab === 'schedules'
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-gray-500 hover:text-gray-700',
          )}
        >
          <CalendarClock className="h-4 w-4" />
          Programmation
        </button>
      </div>

      {tab === 'library' ? <ScriptTable /> : <ScheduleTable />}

      {showCreateModal && tab === 'library' && (
        <ScriptFormModal onClose={() => setShowCreateModal(false)} />
      )}
      {showCreateModal && tab === 'schedules' && (
        <ScheduleFormModal onClose={() => setShowCreateModal(false)} />
      )}
    </div>
  )
}
