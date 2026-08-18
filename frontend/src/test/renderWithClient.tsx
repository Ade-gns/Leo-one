/**
 * renderWithClient.tsx — Helpers de test partagés : rendre un composant, ou
 * un hook, sous un QueryClientProvider frais (retry désactivé, pour des
 * tests rapides et déterministes sur les requêtes/mutations en échec).
 */
import type { ReactElement, ReactNode } from 'react'
import { render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries:   { retry: false },
      mutations: { retry: false },
    },
  })
}

export function renderWithClient(ui: ReactElement, client = createTestQueryClient()) {
  return {
    client,
    ...render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>),
  }
}

/** Wrapper pour renderHook (@testing-library/react) — un hook React Query
 *  ne peut pas être rendu directement sans QueryClientProvider. */
export function createWrapper(client = createTestQueryClient()) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
  }
}
