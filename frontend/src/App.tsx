/**
 * App.tsx — Routeur principal et guard d'authentification
 */
import { lazy, Suspense } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from '@/components/layout/AppShell'
import { useAuthStore, selectIsLoggedIn } from '@/store/auth.store'

const LoginPage      = lazy(() => import('@/pages/LoginPage'))
const DashboardPage  = lazy(() => import('@/pages/DashboardPage'))
const AgentsPage     = lazy(() => import('@/pages/AgentsPage'))
const AgentDetailPage = lazy(() => import('@/pages/AgentDetailPage'))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry:              1,
      staleTime:          30_000,
      refetchOnWindowFocus: true,
    },
  },
})

function RequireAuth({ children }: { children: React.ReactNode }) {
  const isLoggedIn = useAuthStore(selectIsLoggedIn)
  return isLoggedIn ? <>{children}</> : <Navigate to="/login" replace />
}

const PageLoader = () => (
  <div className="flex h-full items-center justify-center">
    <div className="h-8 w-8 animate-spin rounded-full border-4 border-brand-200 border-t-brand-600" />
  </div>
)

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Suspense fallback={<PageLoader />}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />

            <Route
              element={
                <RequireAuth>
                  <AppShell />
                </RequireAuth>
              }
            >
              <Route index                      element={<DashboardPage />}   />
              <Route path="agents"              element={<AgentsPage />}      />
              <Route path="agents/:agentId"     element={<AgentDetailPage />} />
              {/* Routes futures */}
              <Route path="*"                   element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </Suspense>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
