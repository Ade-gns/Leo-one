/**
 * SettingsPage.tsx — Paramètres du tenant courant
 *
 * Pas un CRUD multi-ressources comme Machines/Utilisateurs/Workspaces : un
 * tenant ne se crée/supprime pas en self-service, seuls son nom (ici) et
 * son quota d'agents (lecture seule, fixé par le plan) sont affichés.
 */
import { useState, useEffect } from 'react'
import { Settings, Loader2, Save } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTenant, useUpdateTenant } from '@/hooks/useTenant'

export default function SettingsPage() {
  const { data, isLoading } = useTenant()
  const updateTenant = useUpdateTenant()

  const tenant = data?.data
  const [name, setName] = useState('')
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Initialise le champ une fois les données chargées (et resynchronise si
  // le tenant change côté serveur, ex. modifié depuis un autre onglet).
  useEffect(() => {
    if (tenant) setName(tenant.name)
  }, [tenant])

  const handleSave = () => {
    const trimmed = name.trim()
    if (!trimmed || trimmed === tenant?.name) return
    setError(null)
    setSaved(false)
    updateTenant.mutate(
      { name: trimmed },
      {
        onSuccess: () => { setSaved(true); setTimeout(() => setSaved(false), 2000) },
        onError:   err => setError(err instanceof Error ? err.message : 'Erreur inconnue'),
      },
    )
  }

  const quotaUsedPercent = tenant && tenant.plan_limits.max_agents > 0
    ? Math.min(100, Math.round((tenant.agent_count / tenant.plan_limits.max_agents) * 100))
    : 0

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Settings className="h-6 w-6 text-brand-600" />
        <div>
          <h1 className="text-xl font-bold text-gray-900">Paramètres</h1>
          <p className="text-sm text-gray-500 mt-0.5">Informations du compte</p>
        </div>
      </div>

      {isLoading && (
        <div className="h-40 w-full max-w-lg animate-pulse rounded-xl bg-gray-100" />
      )}

      {!isLoading && tenant && (
        <div className="flex max-w-lg flex-col gap-6 rounded-xl border border-gray-200 bg-white p-6 shadow-sm">

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Nom du compte
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={name}
                onChange={e => setName(e.target.value)}
                className="flex-1 rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />
              <button
                onClick={handleSave}
                disabled={updateTenant.isPending || !name.trim() || name.trim() === tenant.name}
                className="flex items-center gap-2 rounded-lg bg-brand-900 px-4 py-2 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {updateTenant.isPending
                  ? <Loader2 className="h-4 w-4 animate-spin" />
                  : <Save className="h-4 w-4" />
                }
                Enregistrer
              </button>
            </div>
            {saved && <p className="mt-1.5 text-xs text-green-600">Enregistré.</p>}
            {error && <p className="mt-1.5 text-xs text-red-500">Erreur : {error}</p>}
          </div>

          <div className="border-t border-gray-100 pt-6">
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Plan
            </label>
            <p className="text-sm text-gray-900 capitalize">{tenant.plan}</p>
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Agents utilisés
            </label>
            <p className="text-sm text-gray-900">
              {tenant.agent_count} / {tenant.plan_limits.max_agents > 0 ? tenant.plan_limits.max_agents : 'illimité'}
            </p>
            {tenant.plan_limits.max_agents > 0 && (
              <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-gray-100">
                <div
                  className={cn('h-full rounded-full', quotaUsedPercent >= 90 ? 'bg-red-500' : 'bg-brand-600')}
                  style={{ width: `${quotaUsedPercent}%` }}
                />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
