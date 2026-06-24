/**
 * Topbar.tsx — Barre supérieure avec recherche, notifications et profil
 */
import { Bell, Search, LogOut, User } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore, selectUser } from '@/store/auth.store'
import { useUIStore } from '@/store/ui.store'

export function Topbar() {
  const navigate     = useNavigate()
  const user         = useAuthStore(selectUser)
  const { clearSession } = useAuthStore()
  const notifications = useUIStore(s => s.notifications)
  const unreadCount   = notifications.filter(n => n.type === 'error' || n.type === 'warning').length

  const handleLogout = () => {
    clearSession()
    navigate('/login')
  }

  return (
    <header className="flex h-16 shrink-0 items-center gap-4 border-b border-gray-200 bg-white px-6">

      {/* Recherche globale */}
      <div className="flex flex-1 items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 max-w-md">
        <Search className="h-4 w-4 text-gray-400" />
        <input
          type="text"
          placeholder="Rechercher une machine, une alerte…"
          className="flex-1 bg-transparent text-sm text-gray-700 outline-none placeholder:text-gray-400"
        />
      </div>

      <div className="flex items-center gap-2 ml-auto">
        {/* Cloche de notifications */}
        <button
          className="relative rounded-lg p-2 text-gray-500 hover:bg-gray-100"
          aria-label="Notifications"
        >
          <Bell className="h-5 w-5" />
          {unreadCount > 0 && (
            <span className="absolute right-1 top-1 flex h-4 w-4 items-center justify-center rounded-full bg-red-500 text-[10px] font-bold text-white">
              {unreadCount > 9 ? '9+' : unreadCount}
            </span>
          )}
        </button>

        {/* Profil utilisateur */}
        <div className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-1.5">
          <div className="flex h-7 w-7 items-center justify-center rounded-full bg-brand-900 text-white text-xs font-semibold">
            {user?.full_name?.charAt(0).toUpperCase() ?? <User className="h-4 w-4" />}
          </div>
          <span className="text-sm font-medium text-gray-700 hidden md:block">
            {user?.full_name}
          </span>
        </div>

        {/* Déconnexion */}
        <button
          onClick={handleLogout}
          className="rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-red-600"
          aria-label="Se déconnecter"
        >
          <LogOut className="h-5 w-5" />
        </button>
      </div>
    </header>
  )
}
