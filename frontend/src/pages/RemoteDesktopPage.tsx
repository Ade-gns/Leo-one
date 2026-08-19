/**
 * RemoteDesktopPage.tsx — Bureau à distance : crée une session (view ou
 * control, selon ?mode=) au montage, puis affiche le viewer plein cadre
 * pendant sa durée de vie. Route dédiée (pas une modale, contrairement aux
 * autres actions agent) : le canvas vidéo occupe tout l'espace disponible.
 */
import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, useSearchParams } from 'react-router-dom'
import { ChevronLeft, Eye, MousePointer2, Loader2, AlertCircle, Square } from 'lucide-react'
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

  // Compteur de génération : la création de session est asynchrone
  // (mutation React Query), mais le nettoyage de l'effet ci-dessous peut
  // s'exécuter AVANT que la réponse n'arrive — notamment en développement,
  // où StrictMode monte délibérément chaque effet deux fois (monte →
  // nettoie → remonte) pour révéler ce genre de race. Sans ce garde-fou,
  // la session créée par le premier montage reste orpheline côté
  // backend/agent (jamais arrêtée) pendant que le second montage en crée
  // une autre. Incrémenté à chaque démarrage ET à chaque nettoyage : une
  // réponse dont la génération ne correspond plus à la génération courante
  // est d'une session déjà abandonnée, qu'on arrête au lieu d'afficher.
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
    startSession()
    return () => {
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
    <div className="flex h-full flex-col">
      {/* En-tête */}
      <div className="flex items-center justify-between gap-4 border-b border-gray-800 bg-gray-900 px-4 py-3">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate(`/agents/${agentId}`)}
            className="flex items-center gap-1 text-sm text-gray-400 hover:text-white"
          >
            <ChevronLeft className="h-4 w-4" />
            Retour
          </button>
          <div className="h-4 w-px bg-gray-700" />
          <span className="text-sm font-semibold text-white">{agent?.hostname ?? '…'}</span>
          {agent && <AgentStatusBadge status={agent.status} />}
          <span className="rounded-full bg-gray-800 px-2.5 py-0.5 text-xs font-medium text-gray-300">
            {mode === 'control' ? 'Contrôle' : 'Lecture seule'}
          </span>
        </div>

        <div className="flex items-center gap-2">
          {mode === 'view' ? (
            <button
              onClick={() => switchMode('control')}
              className="flex items-center gap-2 rounded-lg bg-brand-700 px-3 py-1.5 text-xs font-semibold text-white hover:bg-brand-600"
            >
              <MousePointer2 className="h-3.5 w-3.5" />
              Prendre le contrôle
            </button>
          ) : (
            <button
              onClick={() => switchMode('view')}
              className="flex items-center gap-2 rounded-lg border border-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-300 hover:bg-gray-800"
            >
              <Eye className="h-3.5 w-3.5" />
              Repasser en lecture seule
            </button>
          )}
          <button
            onClick={() => navigate(`/agents/${agentId}`)}
            className="flex items-center gap-2 rounded-lg border border-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-300 hover:bg-gray-800"
          >
            <Square className="h-3.5 w-3.5" />
            Terminer
          </button>
        </div>
      </div>

      {/* Corps */}
      <div className="flex-1 overflow-hidden bg-black">
        {errorMessage ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-gray-400">
            <AlertCircle className="h-10 w-10 text-red-400" />
            <p className="text-sm">{errorMessage}</p>
            <button
              onClick={startSession}
              className="rounded-lg border border-gray-700 px-4 py-2 text-sm text-gray-300 hover:bg-gray-800"
            >
              Réessayer
            </button>
          </div>
        ) : ended ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-gray-400">
            <p className="text-sm">Session terminée.</p>
            <button
              onClick={startSession}
              className="rounded-lg border border-gray-700 px-4 py-2 text-sm text-gray-300 hover:bg-gray-800"
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
          <div className="flex h-full items-center justify-center gap-3 text-gray-400">
            <Loader2 className="h-6 w-6 animate-spin" />
            <p className="text-sm">Ouverture de la session…</p>
          </div>
        )}
      </div>
    </div>
  )
}
