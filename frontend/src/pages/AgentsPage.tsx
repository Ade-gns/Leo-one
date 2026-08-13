/**
 * AgentsPage.tsx — Liste de toutes les machines supervisées
 */
import { useState } from 'react'
import { Monitor, KeyRound } from 'lucide-react'
import { AgentTable } from '@/components/agents/AgentTable'
import { EnrollmentTokenModal } from '@/components/agents/EnrollmentTokenModal'

export default function AgentsPage() {
  const [showEnrollModal, setShowEnrollModal] = useState(false)

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Monitor className="h-6 w-6 text-brand-600" />
          <div>
            <h1 className="text-xl font-bold text-gray-900">Machines</h1>
            <p className="text-sm text-gray-500 mt-0.5">Gestion et supervision des agents déployés</p>
          </div>
        </div>

        <button
          onClick={() => setShowEnrollModal(true)}
          className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-700"
        >
          <KeyRound className="h-4 w-4" />
          Enrôler un agent
        </button>
      </div>

      <AgentTable />

      {showEnrollModal && (
        <EnrollmentTokenModal onClose={() => setShowEnrollModal(false)} />
      )}
    </div>
  )
}
