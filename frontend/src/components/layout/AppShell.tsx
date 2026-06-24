/**
 * AppShell.tsx — Layout principal de l'application (React Router layout route)
 *
 * Structure :
 *   ┌──────────┬─────────────────────────┐
 *   │ Sidebar  │  Topbar                 │
 *   │          ├─────────────────────────┤
 *   │          │  <Outlet />             │
 *   └──────────┴─────────────────────────┘
 */
import { Outlet } from 'react-router-dom'
import { useUIStore } from '@/store/ui.store'
import { Sidebar }   from './Sidebar'
import { Topbar }    from './Topbar'
import { cn }        from '@/lib/utils'

export function AppShell() {
  const sidebarOpen = useUIStore(s => s.sidebarOpen)

  return (
    <div className="flex h-screen overflow-hidden bg-gray-50">
      <Sidebar />

      <div
        className={cn(
          'flex flex-1 flex-col overflow-hidden transition-all duration-300',
          sidebarOpen ? 'ml-64' : 'ml-16',
        )}
      >
        <Topbar />

        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
