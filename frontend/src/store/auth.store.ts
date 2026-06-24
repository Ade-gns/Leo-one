/**
 * auth.store.ts — État d'authentification global (Zustand)
 *
 * Persiste le token dans sessionStorage pour survivre aux rechargements de page,
 * mais pas entre les onglets (contrairement à localStorage).
 */
import { create } from 'zustand'
import { persist, createJSONStorage } from 'zustand/middleware'
import type { AuthSession, User } from '@/types/user'

interface AuthState {
  session:     AuthSession | null
  accessToken: string | null

  setSession: (session: AuthSession) => void
  clearSession: () => void

  /** Appelle /api/v1/auth/refresh et met à jour le token. Retourne le nouveau token ou null. */
  refresh: () => Promise<string | null>
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      session:     null,
      accessToken: null,

      setSession: (session) => {
        set({ session, accessToken: session.access_token })
      },

      clearSession: () => {
        set({ session: null, accessToken: null })
      },

      refresh: async () => {
        const { session } = get()
        if (!session) return null

        try {
          const res = await fetch('/api/v1/auth/refresh', {
            method:  'POST',
            headers: { 'Content-Type': 'application/json' },
            body:    JSON.stringify({ refresh_token: session.access_token }),
          })

          if (!res.ok) {
            get().clearSession()
            return null
          }

          const json = await res.json() as { data: { access_token: string; expires_in: number } }
          const newToken = json.data.access_token

          set(state => ({
            accessToken: newToken,
            session: state.session
              ? { ...state.session, access_token: newToken }
              : null,
          }))

          return newToken
        } catch {
          get().clearSession()
          return null
        }
      },
    }),
    {
      name:    'leo-auth',
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({
        session:     state.session,
        accessToken: state.accessToken,
      }),
    },
  ),
)

/** Sélecteurs typés pour éviter des re-renders inutiles */
export const selectUser        = (s: AuthState): User | null  => s.session?.user ?? null
export const selectTenantID    = (s: AuthState): string | null => s.session?.user.tenant_id ?? null
export const selectIsLoggedIn  = (s: AuthState): boolean      => s.session !== null
