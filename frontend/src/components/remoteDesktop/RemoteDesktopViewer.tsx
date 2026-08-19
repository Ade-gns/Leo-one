/**
 * RemoteDesktopViewer.tsx — Connexion WebSocket brute vers le relais de
 * bureau à distance (voir backend/internal/infrastructure/remotedesktop),
 * rendu des frames JPEG reçues sur un <canvas>, et (mode "control"
 * seulement) capture souris/clavier renvoyée à l'agent.
 *
 * Protocole binaire (doit correspondre à agent/src/remote_desktop.h et
 * backend/internal/infrastructure/remotedesktop/relay.go) :
 *   0x01 FRAME        agent→ici   : [type][u16 width BE][u16 height BE][u32 seq BE][JPEG...]
 *   0x10 INPUT_MOVE    ici→agent  : [type][u16 x BE][u16 y BE]              (0..65535 normalisé)
 *   0x11 INPUT_BUTTON  ici→agent  : [type][u8 button][u8 down]
 *   0x12 INPUT_SCROLL  ici→agent  : [type][i16 delta BE]
 *   0x13 INPUT_KEY     ici→agent  : [type][u8 leo_rd_key_t][u8 down]
 *   0x20 CONTROL       ici→agent  : [type][JSON]  — {"type":"stop"}
 *
 * En mode "view", aucun listener d'input n'est attaché — défense en
 * profondeur en plus du filtre déjà appliqué par le relais backend et par
 * l'agent lui-même (voir leurs commentaires respectifs).
 */
import { useEffect, useRef, useState } from 'react'
import { WifiOff, Loader2 } from 'lucide-react'
import { leoKeyForCode } from '@/lib/remoteDesktopKeys'
import type { RemoteDesktopMode } from '@/types/remoteDesktop'

const WIRE_FRAME        = 0x01
const WIRE_INPUT_MOVE   = 0x10
const WIRE_INPUT_BUTTON = 0x11
const WIRE_INPUT_SCROLL = 0x12
const WIRE_INPUT_KEY    = 0x13

type ConnectionState = 'connecting' | 'open' | 'closed'

interface RemoteDesktopViewerProps {
  viewerWsUrl: string
  mode: RemoteDesktopMode
  /** Appelé quand la connexion WS se ferme (fin de session, normale ou non). */
  onEnded: () => void
}

/** Convertit une position souris relative au canvas en coordonnées 0..65535
 *  normalisées sur la surface affichée (voir INPUT_MOVE ci-dessus). */
function normalizedCoords(canvas: HTMLCanvasElement, clientX: number, clientY: number) {
  const rect = canvas.getBoundingClientRect()
  const rx = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  const ry = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height))
  return { x: Math.round(rx * 65535), y: Math.round(ry * 65535) }
}

/** button DOM (0=gauche,1=milieu,2=droit) -> convention X11 côté agent (1=gauche,2=milieu,3=droit). */
function leoButton(domButton: number): number {
  return domButton === 0 ? 1 : domButton === 1 ? 2 : 3
}

export function RemoteDesktopViewer({ viewerWsUrl, mode, onEnded }: RemoteDesktopViewerProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const wsRef      = useRef<WebSocket | null>(null)
  const [state, setState] = useState<ConnectionState>('connecting')

  // ── Connexion WS + rendu des frames ────────────────────────────────────
  useEffect(() => {
    const ws = new WebSocket(viewerWsUrl)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen  = () => setState('open')
    ws.onclose = () => { setState('closed'); onEnded() }
    ws.onerror = () => { /* onclose suit toujours onerror pour un WebSocket, rien à faire ici */ }

    ws.onmessage = async (ev: MessageEvent<ArrayBuffer>) => {
      const buf = new Uint8Array(ev.data)
      if (buf.length < 9 || buf[0] !== WIRE_FRAME) return

      const view   = new DataView(ev.data)
      const width  = view.getUint16(1)
      const height = view.getUint16(3)
      const jpegBytes = ev.data.slice(9)

      const canvas = canvasRef.current
      if (!canvas) return

      try {
        const bitmap = await createImageBitmap(new Blob([jpegBytes], { type: 'image/jpeg' }))
        if (canvas.width !== width || canvas.height !== height) {
          canvas.width  = width
          canvas.height = height
        }
        const ctx = canvas.getContext('2d')
        ctx?.drawImage(bitmap, 0, 0)
        bitmap.close()
      } catch {
        // Frame JPEG corrompue/partielle — on l'ignore, la suivante arrive
        // sous ~1/fps secondes (pas la peine de faire échouer la session
        // pour un accroc ponctuel de décodage).
      }
    }

    return () => {
      // Neutralise les callbacks AVANT close() : l'événement 'close' d'un
      // WebSocket est asynchrone (il arrive après ce nettoyage, pas
      // pendant), donc sans ceci onEnded() serait quand même appelé après
      // coup pour CETTE instance — si le parent a entre-temps démarré une
      // nouvelle session (ex: bascule vue → contrôle), cet appel tardif
      // écraserait à tort son état "ended" alors que la nouvelle session,
      // elle, est bien vivante.
      ws.onopen    = null
      ws.onclose   = null
      ws.onerror   = null
      ws.onmessage = null
      ws.close()
      wsRef.current = null
    }
    // onEnded volontairement omis : ne doit pas rouvrir la connexion à chaque re-render du parent
  }, [viewerWsUrl])

  // ── Capture d'input (mode "control" uniquement) ────────────────────────
  useEffect(() => {
    if (mode !== 'control') return
    const canvas = canvasRef.current
    if (!canvas) return

    const send = (bytes: number[]) => {
      const ws = wsRef.current
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(new Uint8Array(bytes))
    }

    const onMouseMove = (e: MouseEvent) => {
      const { x, y } = normalizedCoords(canvas, e.clientX, e.clientY)
      send([WIRE_INPUT_MOVE, (x >> 8) & 0xff, x & 0xff, (y >> 8) & 0xff, y & 0xff])
    }
    const onMouseDown = (e: MouseEvent) => {
      e.preventDefault()
      send([WIRE_INPUT_BUTTON, leoButton(e.button), 1])
    }
    const onMouseUp = (e: MouseEvent) => {
      send([WIRE_INPUT_BUTTON, leoButton(e.button), 0])
    }
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      const notches = Math.min(10, Math.max(1, Math.round(Math.abs(e.deltaY) / 100))) * (e.deltaY > 0 ? -1 : 1)
      send([WIRE_INPUT_SCROLL, (notches >> 8) & 0xff, notches & 0xff])
    }
    const onContextMenu = (e: MouseEvent) => e.preventDefault()

    const onKeyDown = (e: KeyboardEvent) => {
      const key = leoKeyForCode(e.code)
      if (key === null) return
      e.preventDefault()
      send([WIRE_INPUT_KEY, key, 1])
    }
    const onKeyUp = (e: KeyboardEvent) => {
      const key = leoKeyForCode(e.code)
      if (key === null) return
      e.preventDefault()
      send([WIRE_INPUT_KEY, key, 0])
    }

    canvas.addEventListener('mousemove', onMouseMove)
    canvas.addEventListener('mousedown', onMouseDown)
    canvas.addEventListener('mouseup', onMouseUp)
    canvas.addEventListener('wheel', onWheel, { passive: false })
    canvas.addEventListener('contextmenu', onContextMenu)
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)

    return () => {
      canvas.removeEventListener('mousemove', onMouseMove)
      canvas.removeEventListener('mousedown', onMouseDown)
      canvas.removeEventListener('mouseup', onMouseUp)
      canvas.removeEventListener('wheel', onWheel)
      canvas.removeEventListener('contextmenu', onContextMenu)
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
    }
  }, [mode])

  return (
    <div className="relative flex h-full w-full items-center justify-center overflow-auto bg-black">
      <canvas
        ref={canvasRef}
        className="max-h-full max-w-full"
        style={{ cursor: mode === 'control' ? 'none' : 'default' }}
      />
      {state !== 'open' && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black/70 text-white">
          {state === 'connecting' ? (
            <>
              <Loader2 className="h-8 w-8 animate-spin" />
              <p className="text-sm">Connexion à l'agent…</p>
            </>
          ) : (
            <>
              <WifiOff className="h-8 w-8 text-gray-400" />
              <p className="text-sm text-gray-300">Session terminée</p>
            </>
          )}
        </div>
      )}
    </div>
  )
}
