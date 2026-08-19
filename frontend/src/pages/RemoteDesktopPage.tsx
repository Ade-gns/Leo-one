/**
 * RemoteDesktopPage.tsx — Bureau à distance : crée une session (view ou
 * control, selon ?mode=) au montage, puis affiche le viewer plein cadre
 * pendant sa durée de vie. Route dédiée (pas une modale, contrairement aux
 * autres actions agent) : le canvas vidéo occupe tout l'espace disponible.
 */
import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { ChevronLeft, Eye, MousePointer2, Loader2, AlertCircle, Square, MonitorUp, ShieldCheck } from 'lucide-react'
import { useAgent } from '@/hooks/useAgents'
import { useCreateRemoteDesktopSession } from '@/hooks/useRemoteDesktop'
import { remoteDesktopApi } from '@/api/remoteDesktop'
import { ApiRequestError } from '@/api/client'
import { RemoteDesktopViewer } from '@/components/remoteDesktop/RemoteDesktopViewer'
import { AgentStatusBadge } from '@/components/agents/AgentStatusBadge'
import type { RemoteDesktopMode } from '@/types/remoteDesktop'

interface ActiveSession {
  sessionId: string
  viewerWsUrl: string
}

export default function RemoteDesktopPage() {
  const { agentId } = useParams<{ agentId: string }>()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const mode: RemoteDesktopMode = searchParams.get('mode') === 'control' ? 'control' : 'view'

  const { data: agentData } = useAgent(agentId!)
  const agent = agentData?.data

  const createSession = useCreateRemoteDesktopSession(agentId!, mode)
  const [session, setSession] = useState<ActiveSession | null>(null)
  const [ended, setEnded] = useState(false)
  const sessionIdRef = useRef<string | null>(null)

  // Compteur de génération : filet de sécurité pour le cas où l'effet
  // ci-dessous se démonte pour de bon (navigation réelle) pendant qu'une
  // création de session est encore en vol — la réponse tardive est alors
  // arrêtée au lieu d'être affichée. Incrémenté à chaque démarrage ET à
  // chaque nettoyage.
  //
  // Ne suffit PAS, à lui seul, à couvrir le double-montage StrictMode
  // (monte → nettoie → remonte, développement uniquement — mais aussi en
  // "production" tant que docker-compose sert le frontend via `vite dev`,
  // voir infra/docker/frontend.Dockerfile) : les DEUX montages envoient
  // chacun un vrai POST de création de session, dont l'un est accepté par
  // l'agent (une seule session active à la fois, arbitrée côté agent selon
  // l'ORDRE D'ARRIVÉE de la commande START sur le canal de contrôle) et
  // l'autre rejeté. Rien ne garantit que cet ordre corresponde à l'ordre
  // des réponses HTTP reçues par le navigateur (planification réseau/
  // goroutines indépendante) — un vrai test bureau à distance de bout en
  // bout a confirmé le cas où le générateur ci-dessous, se fiant à l'ordre
  // de réponse, arrêtait la session que l'agent avait justement acceptée
  // et gardait celle qu'il avait rejetée : plus aucune paire agent/
  // navigateur possible, la session orpheline restait bloquée
  // "SESSION_ALREADY_ACTIVE" jusqu'à l'expiration du pairTimeout (30s) côté
  // relais. D'où le setTimeout(0) dans l'effet : il empêche le premier
  // montage StrictMode d'émettre la moindre requête avant d'être nettoyé,
  // au lieu d'essayer de démêler après coup deux requêtes déjà parties.
  const generationRef = useRef(0)

  // Démarre une nouvelle session (réutilisé au montage, au changement de
  // mode, et par les boutons "Réessayer"/"Nouvelle session"). Appel direct
  // à remoteDesktopApi (pas le hook de mutation) pour l'arrêt best-effort :
  // évite toute dépendance sur une référence de mutation, qui change à
  // chaque rendu.
  const startSession = () => {
    if (!agentId) return
    const myGeneration = ++generationRef.current
    setSession(null)
    setEnded(false)
    createSession.mutate(undefined, {
      onSuccess: res => {
        if (generationRef.current !== myGeneration) {
          void remoteDesktopApi.stopSession(agentId, res.data.session_id)
          return
        }
        sessionIdRef.current = res.data.session_id
        setSession({ sessionId: res.data.session_id, viewerWsUrl: res.data.viewer_ws_url })
      },
    })
  }

  useEffect(() => {
    // Différé d'un tick : le double-montage StrictMode (monte -> nettoie ->
    // remonte) s'exécute entièrement de façon synchrone, avant qu'aucun
    // timer ne puisse se déclencher. Le nettoyage du premier montage annule
    // donc ce timer avant qu'il n'appelle startSession() — ce montage
    // jetable n'envoie alors jamais de requête, il n'y a plus deux sessions
    // à départager après coup (voir le commentaire de generationRef).
    const timer = setTimeout(startSession, 0)
    return () => {
      clearTimeout(timer)
      generationRef.current++
      if (agentId && sessionIdRef.current) {
        void remoteDesktopApi.stopSession(agentId, sessionIdRef.current)
        sessionIdRef.current = null
      }
    }
    // startSession volontairement omis (dépend de createSession, une mutation dont la référence change à chaque rendu) ; ne doit tourner qu'au changement d'agent/mode
  }, [agentId, mode])

  const switchMode = (next: RemoteDesktopMode) => {
    navigate(`/agents/${agentId}/remote-desktop${next === 'control' ? '?mode=control' : ''}`, { replace: true })
  }

  const errorMessage = createSession.isError
    ? createSession.error instanceof ApiRequestError
      ? createSession.error.message
      : 'Erreur inconnue'
    : null

  return (
    <div className="flex h-full min-h-[560px] flex-col bg-slate-950">
      {/* En-tête */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 bg-slate-950 px-4 py-3 shadow-xl shadow-black/20">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(`/agents/${agentId}`)}
            className="flex items-center gap-1 rounded-lg px-1 py-1 text-sm text-slate-400 transition hover:text-white"
          >
            <ChevronLeft className="h-4 w-4" />
            Retour
          </button>
          <div className="hidden h-5 w-px bg-white/10 sm:block" />
          <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-brand-500/15 text-blue-200"><MonitorUp className="h-4 w-4" /></span>
          <div>
            <span className="block text-sm font-semibold text-white">{agent?.hostname ?? '…'}</span>
            <span className="hidden text-xs text-slate-500 sm:block">Session sécurisée</span>
          </div>
          {agent && <AgentStatusBadge status={agent.status} />}
          <span className={`rounded-full border px-2.5 py-1 text-xs font-semibold ${mode === 'control' ? 'border-amber-400/25 bg-amber-400/10 text-amber-200' : 'border-emerald-400/20 bg-emerald-400/10 text-emerald-200'}`}>
            {mode === 'control' ? 'Contrôle actif' : 'Lecture seule'}
          </span>
        </div>

        <div className="flex items-center gap-2">
          {mode === 'view' ? (
            <button
              onClick={() => switchMode('control')}
              className="flex items-center gap-2 rounded-xl bg-brand-600 px-3 py-2 text-xs font-semibold text-white shadow-lg shadow-brand-950/30 transition hover:bg-brand-500"
            >
              <MousePointer2 className="h-3.5 w-3.5" />
              Prendre le contrôle
            </button>
          ) : (
            <button
              onClick={() => switchMode('view')}
              className="flex items-center gap-2 rounded-xl border border-white/15 px-3 py-2 text-xs font-semibold text-slate-200 transition hover:bg-white/10"
            >
              <Eye className="h-3.5 w-3.5" />
              Repasser en lecture seule
            </button>
          )}
          <button
            onClick={() => navigate(`/agents/${agentId}`)}
            className="flex items-center gap-2 rounded-xl border border-white/15 px-3 py-2 text-xs font-semibold text-slate-200 transition hover:border-red-400/40 hover:bg-red-500/10 hover:text-red-100"
          >
            <Square className="h-3.5 w-3.5" />
            Terminer
          </button>
        </div>
      </div>

      {/* Corps */}
      <div className="relative flex-1 overflow-hidden bg-[radial-gradient(ellipse_at_center,_#172554_0%,_#020617_66%)] p-3 sm:p-5">
        <div className="pointer-events-none absolute left-5 top-4 hidden items-center gap-2 text-[11px] font-medium tracking-wide text-slate-500 sm:flex">
          <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" /> Flux chiffré
        </div>
        {errorMessage ? (
          <div className="surface-card mx-auto flex h-full max-h-80 max-w-md flex-col items-center justify-center gap-3 p-8 text-slate-500">
            <AlertCircle className="h-10 w-10 text-red-400" />
            <p className="text-sm">{errorMessage}</p>
            <button
              onClick={startSession}
              className="rounded-xl bg-slate-900 px-4 py-2 text-sm font-semibold text-white transition hover:bg-slate-800"
            >
              Réessayer
            </button>
          </div>
        ) : ended ? (
          <div className="surface-card mx-auto flex h-full max-h-80 max-w-md flex-col items-center justify-center gap-3 p-8 text-slate-500">
            <p className="text-sm font-medium text-slate-700">Session terminée.</p>
            <button
              onClick={startSession}
              className="rounded-xl bg-slate-900 px-4 py-2 text-sm font-semibold text-white transition hover:bg-slate-800"
            >
              Nouvelle session
            </button>
          </div>
        ) : session ? (
          <RemoteDesktopViewer
            key={session.sessionId}
            viewerWsUrl={session.viewerWsUrl}
            mode={mode}
            onEnded={() => {
              sessionIdRef.current = null
              setEnded(true)
            }}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-slate-300">
            <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-white/10 ring-1 ring-white/10"><Loader2 className="h-6 w-6 animate-spin text-blue-300" /></span>
            <p className="text-sm font-medium">Ouverture de la session…</p>
            <p className="text-xs text-slate-500">Connexion sécurisée à l’agent</p>
          </div>
        )}
      </div>
    </div>
  )
}
