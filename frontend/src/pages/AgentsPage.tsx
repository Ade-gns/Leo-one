/**
 * AgentsPage.tsx — Liste de toutes les machines supervisées
 */
import { Monitor } from 'lucide-react'
import { AgentTable } from '@/components/agents/AgentTable'

export default function AgentsPage() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Monitor className="h-6 w-6 text-brand-600" />
        <div>
          <h1 className="text-xl font-bold text-gray-900">Machines</h1>
          <p className="text-sm text-gray-500 mt-0.5">Gestion et supervision des agents déployés</p>
        </div>
      </div>

      <AgentTable />
    </div>
  )
}
