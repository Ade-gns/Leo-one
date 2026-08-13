/**
 * EnrollmentTokenModal.tsx — Génération et gestion des tokens d'enrôlement
 *
 * Un token d'enrôlement est consommé une seule fois par un nouvel agent
 * (POST /api/v1/enroll côté agent — voir agent/src/enroll.c) pour obtenir
 * son identité complète (agent_id/tenant_id/certificat client mTLS). Le
 * token brut n'est renvoyé qu'à la création, ici : seul son hash est
 * conservé côté backend, il ne peut donc plus jamais être raffiché après
 * fermeture de cette modale.
 */
import { useState } from 'react'
import { X, KeyRound, Loader2, Plus, Copy, Check, Trash2 } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { cn } from '@/lib/utils'
import {
  useEnrollmentTokens, useCreateEnrollmentToken, useDeleteEnrollmentToken,
} from '@/hooks/useEnrollmentTokens'
import type { EnrollmentToken, EnrollmentTokenCreateResponse } from '@/types/enrollmentToken'

interface EnrollmentTokenModalProps {
  onClose: () => void
}

function tokenStatus(token: EnrollmentToken): { label: string; bg: string; text: string } {
  if (token.used_at) return { label: 'Utilisé', bg: 'bg-gray-100',  text: 'text-gray-600'  }
  if (new Date(token.expires_at).getTime() < Date.now())
    return { label: 'Expiré',  bg: 'bg-red-50',   text: 'text-red-600'   }
  return { label: 'Actif', bg: 'bg-green-50', text: 'text-green-700' }
}

export function EnrollmentTokenModal({ onClose }: EnrollmentTokenModalProps) {
  const [label, setLabel]     = useState('')
  const [ttlHours, setTtlHours] = useState('')
  const [created, setCreated] = useState<EnrollmentTokenCreateResponse | null>(null)
  const [copied, setCopied]   = useState(false)
  const [error, setError]     = useState<string | null>(null)

  const { data, isLoading } = useEnrollmentTokens()
  const createToken = useCreateEnrollmentToken()
  const deleteToken  = useDeleteEnrollmentToken()

  const tokens = data?.data ?? []

  const handleCreate = () => {
    setError(null)
    createToken.mutate(
      {
        label: label.trim() || undefined,
        expires_in_hours: ttlHours ? Number(ttlHours) : undefined,
      },
      {
        onSuccess: res => {
          setCreated(res.data)
          setLabel('')
          setTtlHours('')
        },
        onError: err => {
          setError(err instanceof Error ? err.message : 'Erreur inconnue')
        },
      },
    )
  }

  const handleCopy = () => {
    if (!created) return
    navigator.clipboard.writeText(created.token).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-lg max-h-[85vh] flex-col rounded-2xl bg-white shadow-2xl">

        {/* En-tête */}
        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <KeyRound className="h-5 w-5 text-brand-600" />
            <div>
              <h2 className="text-base font-semibold text-gray-900">Enrôler un agent</h2>
              <p className="text-xs text-gray-400">Générer et gérer les tokens d'enrôlement</p>
            </div>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-5 overflow-y-auto p-6">

          {/* Token fraîchement créé — affiché une seule fois */}
          {created && (
            <div className="rounded-lg border border-green-200 bg-green-50 p-4">
              <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-green-700">
                Token généré — à copier maintenant, il ne sera plus jamais affiché
              </p>
              <div className="flex items-center gap-2">
                <code className="flex-1 overflow-x-auto whitespace-nowrap rounded-lg bg-gray-950 px-3 py-2 font-mono text-xs text-green-400">
                  {created.token}
                </code>
                <button
                  onClick={handleCopy}
                  className="flex shrink-0 items-center gap-1.5 rounded-lg border border-green-300 bg-white px-3 py-2 text-xs font-semibold text-green-700 hover:bg-green-100"
                >
                  {copied
                    ? <><Check className="h-3.5 w-3.5" />Copié</>
                    : <><Copy className="h-3.5 w-3.5" />Copier</>
                  }
                </button>
              </div>
              <p className="mt-2 text-xs text-gray-500">
                À placer dans <code className="font-mono">agent_bootstrap.conf</code> sur la machine
                à enrôler (clé <code className="font-mono">enrollment_token</code>), avec l'URL de l'API
                (clé <code className="font-mono">api_endpoint</code>).
              </p>
            </div>
          )}

          {/* Formulaire de création */}
          <div className="flex flex-col gap-3">
            <div className="flex gap-3">
              <div className="flex-1">
                <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                  Libellé (optionnel)
                </label>
                <input
                  type="text"
                  value={label}
                  onChange={e => setLabel(e.target.value)}
                  placeholder="ex : PARIS-SRV-02"
                  className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                />
              </div>
              <div className="w-32">
                <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                  Validité (h)
                </label>
                <input
                  type="number"
                  min={1}
                  value={ttlHours}
                  onChange={e => setTtlHours(e.target.value)}
                  placeholder="24"
                  className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                />
              </div>
            </div>

            {error && <p className="text-xs text-red-500">Erreur : {error}</p>}

            <button
              onClick={handleCreate}
              disabled={createToken.isPending}
              className="flex items-center justify-center gap-2 self-end rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {createToken.isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Génération…</>
                : <><Plus className="h-4 w-4" />Générer un token</>
              }
            </button>
          </div>

          {/* Liste des tokens existants */}
          <div>
            <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">
              Tokens existants
            </p>
            <div className="overflow-hidden rounded-lg border border-gray-200">
              {isLoading && (
                <div className="space-y-2 p-3">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <div key={i} className="h-8 w-full animate-pulse rounded bg-gray-100" />
                  ))}
                </div>
              )}

              {!isLoading && tokens.length === 0 && (
                <p className="p-4 text-center text-xs text-gray-400">Aucun token pour l'instant</p>
              )}

              {!isLoading && tokens.map(token => {
                const status = tokenStatus(token)
                return (
                  <div
                    key={token.id}
                    className="flex items-center justify-between gap-3 border-b border-gray-50 px-3 py-2.5 last:border-b-0 hover:bg-gray-50"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium text-gray-900">
                        {token.label || <span className="italic text-gray-400">Sans libellé</span>}
                      </p>
                      <p className="text-xs text-gray-400">
                        Créé {formatDistanceToNow(new Date(token.created_at), { addSuffix: true, locale: fr })}
                      </p>
                    </div>
                    <span className={cn('shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold', status.bg, status.text)}>
                      {status.label}
                    </span>
                    <button
                      onClick={() => {
                        if (confirm('Révoquer ce token ? Il ne pourra plus être utilisé pour enrôler un agent.')) {
                          deleteToken.mutate(token.id)
                        }
                      }}
                      className="shrink-0 rounded p-1.5 text-gray-400 hover:bg-red-50 hover:text-red-600"
                      title="Révoquer"
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                )
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
