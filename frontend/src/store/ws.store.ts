/**
 * ws.store.ts — État de la connexion WebSocket (Zustand)
 *
 * La connexion WS côté frontend sert à recevoir les mises à jour
 * de métriques en temps réel pushées par le backend.
 * (Distinct du WS agent : celui-ci est côté navigateur/tableau de bord)
 */
import { create } from 'zustand'
import type { LatestMetrics } from '@/types/metric'

type WSStatus = 'disconnected' | 'connecting' | 'connected' | 'error'

interface WSState {
  status:      WSStatus
  socket:      WebSocket | null
  liveMetrics: Record<string, LatestMetrics>   /* agent_id → dernières métriques */

  connect:        (url: string, token: string) => void
  disconnect:     () => void
  updateMetrics:  (agentID: string, metrics: LatestMetrics) => void
}

export const useWSStore = create<WSState>((set, get) => ({
  status:      'disconnected',
  socket:      null,
  liveMetrics: {},

  connect: (url, token) => {
    get().disconnect()

    set({ status: 'connecting' })
    const ws = new WebSocket(`${url}?token=${encodeURIComponent(token)}`)

    ws.onopen = () => set({ status: 'connected', socket: ws })

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data as string) as {
          type:     string
          agent_id: string
          metrics:  LatestMetrics
        }
        if (msg.type === 'LIVE_METRICS' && msg.agent_id) {
          get().updateMetrics(msg.agent_id, msg.metrics)
        }
      } catch { /* message non-JSON ignoré */ }
    }

    ws.onclose  = () => set({ status: 'disconnected', socket: null })
    ws.onerror  = () => set({ status: 'error' })

    set({ socket: ws })
  },

  disconnect: () => {
    const { socket } = get()
    if (socket && socket.readyState < WebSocket.CLOSING) {
      socket.close()
    }
    set({ socket: null, status: 'disconnected' })
  },

  updateMetrics: (agentID, metrics) =>
    set(s => ({
      liveMetrics: { ...s.liveMetrics, [agentID]: metrics },
    })),
}))
