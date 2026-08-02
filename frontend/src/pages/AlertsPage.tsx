/**
 * AlertsPage.tsx — Liste des alertes déclenchées sur l'infrastructure
 */
import { Bell } from 'lucide-react'
import { AlertTable } from '@/components/alerts/AlertTable'

export default function AlertsPage() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Bell className="h-6 w-6 text-brand-600" />
        <div>
          <h1 className="text-xl font-bold text-gray-900">Alertes</h1>
          <p className="text-sm text-gray-500 mt-0.5">Supervision des alertes déclenchées sur votre infrastructure</p>
        </div>
      </div>

      <AlertTable />
    </div>
  )
}
