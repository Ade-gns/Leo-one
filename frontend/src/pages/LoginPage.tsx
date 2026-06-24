/**
 * LoginPage.tsx — Page d'authentification avec email/mot de passe + TOTP
 */
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Shield, Loader2, Eye, EyeOff } from 'lucide-react'
import { post } from '@/api/client'
import { useAuthStore } from '@/store/auth.store'
import type { AuthSession } from '@/types/user'

type Step = 'credentials' | 'totp'

export default function LoginPage() {
  const navigate  = useNavigate()
  const { setSession } = useAuthStore()

  const [step, setStep]             = useState<Step>('credentials')
  const [email, setEmail]           = useState('')
  const [password, setPassword]     = useState('')
  const [showPass, setShowPass]     = useState(false)
  const [totp, setTotp]             = useState('')
  const [mfaToken, setMfaToken]     = useState('')
  const [error, setError]           = useState<string | null>(null)
  const [loading, setLoading]       = useState(false)

  const handleCredentials = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const res = await post<{ mfa_required: boolean; mfa_token?: string; session?: AuthSession }>(
        '/auth/login', { email, password },
      )
      if (res.mfa_required && res.mfa_token) {
        setMfaToken(res.mfa_token)
        setStep('totp')
      } else if (res.session) {
        setSession(res.session)
        navigate('/', { replace: true })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Identifiants invalides')
    } finally {
      setLoading(false)
    }
  }

  const handleTotp = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const res = await post<{ session: AuthSession }>(
        '/auth/mfa/verify', { mfa_token: mfaToken, code: totp },
      )
      setSession(res.session)
      navigate('/', { replace: true })
    } catch (err) {
      setError('Code invalide ou expiré')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm">

        {/* Logo */}
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-900 shadow-lg">
            <Shield className="h-8 w-8 text-blue-400" />
          </div>
          <div className="text-center">
            <h1 className="text-2xl font-bold text-gray-900">Leo-One</h1>
            <p className="text-sm text-gray-500">Remote Monitoring & Management</p>
          </div>
        </div>

        <div className="rounded-2xl border border-gray-200 bg-white p-8 shadow-sm">

          {step === 'credentials' && (
            <form onSubmit={handleCredentials} className="space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1.5">
                  Adresse e-mail
                </label>
                <input
                  type="email"
                  required
                  autoComplete="email"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  className="w-full rounded-lg border border-gray-200 px-3 py-2.5 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1.5">
                  Mot de passe
                </label>
                <div className="relative">
                  <input
                    type={showPass ? 'text' : 'password'}
                    required
                    autoComplete="current-password"
                    value={password}
                    onChange={e => setPassword(e.target.value)}
                    className="w-full rounded-lg border border-gray-200 px-3 py-2.5 pr-10 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPass(v => !v)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600"
                  >
                    {showPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>

              {error && (
                <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{error}</p>
              )}

              <button
                type="submit"
                disabled={loading}
                className="flex w-full items-center justify-center gap-2 rounded-lg bg-brand-900 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50"
              >
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}
                {loading ? 'Connexion…' : 'Se connecter'}
              </button>
            </form>
          )}

          {step === 'totp' && (
            <form onSubmit={handleTotp} className="space-y-5">
              <div className="text-center">
                <h2 className="font-semibold text-gray-800">Vérification à deux facteurs</h2>
                <p className="text-sm text-gray-500 mt-1">
                  Entrez le code de votre application d'authentification
                </p>
              </div>

              <input
                type="text"
                inputMode="numeric"
                pattern="[0-9]{6}"
                maxLength={6}
                required
                placeholder="000000"
                value={totp}
                onChange={e => setTotp(e.target.value.replace(/\D/g, ''))}
                className="w-full rounded-lg border border-gray-200 px-3 py-2.5 text-center text-2xl font-mono tracking-[0.5em] outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />

              {error && (
                <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{error}</p>
              )}

              <button
                type="submit"
                disabled={loading || totp.length !== 6}
                className="flex w-full items-center justify-center gap-2 rounded-lg bg-brand-900 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50"
              >
                {loading && <Loader2 className="h-4 w-4 animate-spin" />}
                {loading ? 'Vérification…' : 'Valider'}
              </button>

              <button
                type="button"
                onClick={() => { setStep('credentials'); setError(null) }}
                className="w-full text-center text-sm text-gray-400 hover:text-gray-600"
              >
                Retour
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
