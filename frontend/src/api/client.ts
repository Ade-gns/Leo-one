/**
 * client.ts — Instance fetch configurée pour l'API Leo-One
 *
 * Responsabilités :
 *  - Injecte l'Authorization header (Bearer JWT) automatiquement
 *  - Rafraîchit le token en cas de 401 (une seule tentative)
 *  - Normalise les erreurs en ApiError
 *  - Gère la pagination cursor-based
 */
import type { ApiError } from '@/types/api'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

/** Classe d'erreur typée pour les erreurs API */
export class ApiRequestError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message)
    this.name = 'ApiRequestError'
  }
}

/** Récupère le token courant depuis le store Zustand (import dynamique pour éviter le cycle) */
async function getToken(): Promise<string | null> {
  const { useAuthStore } = await import('@/store/auth.store')
  return useAuthStore.getState().accessToken
}

/** Tente un refresh du token et retourne le nouveau token ou null */
async function tryRefreshToken(): Promise<string | null> {
  const { useAuthStore } = await import('@/store/auth.store')
  return useAuthStore.getState().refresh()
}

/** Client fetch de base avec injection du JWT et gestion 401 */
async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const token = await getToken()

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  let response = await fetch(`${API_BASE}${path}`, { ...options, headers })

  // Tentative de refresh automatique sur 401
  if (response.status === 401 && token) {
    const newToken = await tryRefreshToken()
    if (newToken) {
      headers['Authorization'] = `Bearer ${newToken}`
      response = await fetch(`${API_BASE}${path}`, { ...options, headers })
    }
  }

  if (!response.ok) {
    let body: ApiError | null = null
    try { body = await response.json() } catch { /* corps non-JSON */ }
    throw new ApiRequestError(
      body?.error?.code ?? 'UNKNOWN_ERROR',
      body?.error?.message ?? `HTTP ${response.status}`,
      response.status,
    )
  }

  // 204 No Content
  if (response.status === 204) return undefined as T

  return response.json() as Promise<T>
}

/** GET */
export const get  = <T>(path: string, params?: Record<string, string>) => {
  const url = params
    ? `${path}?${new URLSearchParams(params).toString()}`
    : path
  return request<T>(url)
}

/** POST */
export const post = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined })

/** PATCH */
export const patch = <T>(path: string, body?: unknown) =>
  request<T>(path, { method: 'PATCH', body: body ? JSON.stringify(body) : undefined })

/** DELETE */
export const del = <T>(path: string) =>
  request<T>(path, { method: 'DELETE' })
