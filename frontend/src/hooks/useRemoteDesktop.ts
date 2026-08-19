/**
 * useRemoteDesktop.ts — Hooks React Query pour le bureau à distance
 */
import { useMutation } from '@tanstack/react-query'
import { remoteDesktopApi } from '@/api/remoteDesktop'
import type { RemoteDesktopMode } from '@/types/remoteDesktop'

/** Crée une session (view ou control) sur un agent. */
export function useCreateRemoteDesktopSession(agentID: string, mode: RemoteDesktopMode) {
  return useMutation({
    mutationFn: () =>
      mode === 'control'
        ? remoteDesktopApi.createControlSession(agentID)
        : remoteDesktopApi.createViewSession(agentID),
  })
}

/** Arrête une session en cours — best-effort, appelé à la fermeture de la page/onglet. */
export function useStopRemoteDesktopSession(agentID: string) {
  return useMutation({
    mutationFn: (sessionID: string) => remoteDesktopApi.stopSession(agentID, sessionID),
  })
}
