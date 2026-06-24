/**
 * Sidebar.tsx — Navigation latérale principale
 */
import { NavLink } from 'react-router-dom'
import {
  LayoutDashboard,
  Monitor,
  Bell,
  Ticket,
  Settings,
  Users,
  ChevronLeft,
  ChevronRight,
  Shield,
} from 'lucide-react'
import { useUIStore } from '@/store/ui.store'
import { cn } from '@/lib/utils'

interface NavItem {
  label:    string
  to:       string
  icon:     React.ElementType
  badge?:   number
}

const NAV_ITEMS: NavItem[] = [
  { label: 'Tableau de bord', to: '/',        icon: LayoutDashboard },
  { label: 'Machines',        to: '/agents',  icon: Monitor         },
  { label: 'Alertes',         to: '/alerts',  icon: Bell            },
  { label: 'Tickets',         to: '/tickets', icon: Ticket          },
  { label: 'Utilisateurs',    to: '/users',   icon: Users           },
  { label: 'Paramètres',      to: '/settings',icon: Settings        },
]

export function Sidebar() {
  const { sidebarOpen, toggleSidebar } = useUIStore()

  return (
    <aside
      className={cn(
        'fixed inset-y-0 left-0 z-40 flex flex-col bg-brand-900 text-white',
        'transition-all duration-300',
        sidebarOpen ? 'w-64' : 'w-16',
      )}
    >
      {/* Logo */}
      <div className="flex h-16 items-center gap-3 border-b border-white/10 px-4">
        <Shield className="h-7 w-7 shrink-0 text-blue-400" />
        {sidebarOpen && (
          <span className="text-lg font-bold tracking-tight">Leo-One</span>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 overflow-y-auto px-2 py-4">
        {NAV_ITEMS.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) => cn(
              'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium',
              'transition-colors duration-150',
              isActive
                ? 'bg-white/20 text-white'
                : 'text-white/70 hover:bg-white/10 hover:text-white',
            )}
          >
            <item.icon className="h-5 w-5 shrink-0" />
            {sidebarOpen && (
              <span className="flex-1">{item.label}</span>
            )}
            {sidebarOpen && item.badge !== undefined && (
              <span className="rounded-full bg-red-500 px-2 py-0.5 text-xs font-semibold">
                {item.badge}
              </span>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Toggle sidebar */}
      <div className="border-t border-white/10 p-2">
        <button
          onClick={toggleSidebar}
          className="flex w-full items-center justify-center rounded-lg p-2 text-white/50 hover:bg-white/10 hover:text-white"
          aria-label={sidebarOpen ? 'Réduire le menu' : 'Agrandir le menu'}
        >
          {sidebarOpen ? <ChevronLeft className="h-5 w-5" /> : <ChevronRight className="h-5 w-5" />}
        </button>
      </div>
    </aside>
  )
}
