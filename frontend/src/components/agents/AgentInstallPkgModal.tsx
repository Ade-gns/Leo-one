/**
 * AgentInstallPkgModal.tsx — Modal pour installer un ou plusieurs paquets sur un agent
 *
 * Envoie une commande install_pkg (voir agent/src/agent.c _dispatch_install_pkg) :
 * l'agent exécute apt-get update && apt-get install directement en argv
 * (pas de shell — voir la revue de code du 2026-08-13), donc les noms de
 * paquets ne sont jamais interprétés comme du texte de commande. La
 * validation faite ici côté UI (charset restreint) n'est qu'un confort —
 * la vraie barrière est côté agent (_pkg_name_valid) et l'absence de shell.
 */
import { useState } from 'react'
import { X, Package, Loader2, Plus } from 'lucide-react'
import { useInstallPkg } from '@/hooks/useAgents'
import type { InstallPkgPayload } from '@/types/agent'

interface AgentInstallPkgModalProps {
  agentId:  string
  hostname: string
  onClose:  () => void
}

// Même charset que _pkg_name_valid côté agent (alphanumérique + . + - _ :) —
// avertit tôt plutôt que de laisser l'agent rejeter silencieusement plus tard.
const PKG_NAME_RE = /^[a-zA-Z0-9.+_:-]+$/

export function AgentInstallPkgModal({ agentId, hostname, onClose }: AgentInstallPkgModalProps) {
  const [input, setInput]   = useState('')
  const [output, setOutput] = useState<string | null>(null)

  const installPkg = useInstallPkg(agentId)

  const packages = input
    .split(/[\n,]/)
    .map(s => s.trim())
    .filter(Boolean)

  const invalidPackages = packages.filter(p => !PKG_NAME_RE.test(p))

  const handleInstall = () => {
    if (packages.length === 0 || invalidPackages.length > 0) return
    setOutput(null)
    const payload: InstallPkgPayload = { packages, timeout_sec: 300 }
    installPkg.mutate(payload, {
      onSuccess: data => {
        setOutput(data.data?.stdout ?? data.data?.stderr ?? '(aucune sortie)')
      },
      onError: err => {
        setOutput(`Erreur : ${err instanceof Error ? err.message : 'inconnue'}`)
      },
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-lg flex-col rounded-2xl bg-white shadow-2xl">

        {/* En-tête */}
        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <Package className="h-5 w-5 text-brand-600" />
            <div>
              <h2 className="text-base font-semibold text-gray-900">Installer un paquet</h2>
              <p className="text-xs text-gray-400">{hostname}</p>
            </div>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Paquets (un par ligne, ou séparés par une virgule)
            </label>
            <textarea
              value={input}
              onChange={e => setInput(e.target.value)}
              placeholder={'firefox\nhtop'}
              rows={4}
              className="w-full resize-none rounded-lg border border-gray-200 bg-gray-950 px-4 py-3 font-mono text-sm text-green-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
            {invalidPackages.length > 0 && (
              <p className="mt-1.5 text-xs text-red-500">
                Nom(s) de paquet invalide(s) : {invalidPackages.join(', ')}
              </p>
            )}
            {packages.length > 0 && invalidPackages.length === 0 && (
              <p className="mt-1.5 text-xs text-gray-400">
                {packages.length} paquet{packages.length > 1 ? 's' : ''} à installer via apt-get.
              </p>
            )}
          </div>

          {/* Bouton Installer */}
          <div className="flex justify-end">
            <button
              onClick={handleInstall}
              disabled={packages.length === 0 || invalidPackages.length > 0 || installPkg.isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {installPkg.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Installation…</>
                : <><Plus className="h-4 w-4" />Installer</>
              }
            </button>
          </div>

          {/* Sortie */}
          {output !== null && (
            <div className="rounded-lg border border-gray-200 bg-gray-950 p-4">
              <p className="mb-1 text-xs font-semibold text-gray-400 uppercase tracking-wider">Sortie</p>
              <pre className="whitespace-pre-wrap text-xs text-green-400 font-mono max-h-48 overflow-y-auto">
                {output}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
