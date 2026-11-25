import * as React from 'react'
import { createFileRoute, redirect, useRouter } from '@tanstack/react-router'
import { useServerFn } from '@tanstack/react-start'
import { ArrowRight, Lock, Mail, ShieldCheck } from 'lucide-react'
import { getSessionFn, loginFn } from '~/server/session'
import { Button, Input, Label } from '~/components/ui'

export const Route = createFileRoute('/login')({
  beforeLoad: async () => {
    const session = await getSessionFn()
    if (session) throw redirect({ to: '/' })
  },
  component: LoginPage,
})

function LoginPage() {
  const router = useRouter()
  const login = useServerFn(loginFn)
  const [email, setEmail] = React.useState('')
  const [password, setPassword] = React.useState('')
  const [error, setError] = React.useState('')
  const [pending, setPending] = React.useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    setError('')
    try {
      const result = await login({ data: { email, password } })
      if (!result.ok) {
        setError(result.error ?? 'Sign in failed.')
        return
      }
      await router.invalidate()
      await router.navigate({ to: '/' })
    } catch {
      setError('Unable to reach the archive engine. Try again.')
    } finally {
      setPending(false)
    }
  }

  return (
    <main className="grid min-h-screen bg-slate-950 lg:grid-cols-[1.05fr_0.95fr]">
      <section className="relative hidden overflow-hidden border-r border-white/10 lg:flex lg:flex-col lg:justify-between lg:p-14">
        <div
          className="absolute inset-0 opacity-[0.16]"
          style={{
            backgroundImage:
              'linear-gradient(to right, rgb(148 163 184 / 0.25) 1px, transparent 1px), linear-gradient(to bottom, rgb(148 163 184 / 0.25) 1px, transparent 1px)',
            backgroundSize: '44px 44px',
            maskImage: 'radial-gradient(ellipse at 30% 30%, black 20%, transparent 75%)',
          }}
        />
        <div className="relative">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-500 text-white shadow-lg shadow-indigo-500/30">
              <ShieldCheck className="h-5 w-5" />
            </div>
            <div>
              <p className="text-base font-semibold tracking-tight text-white">PocketArchive</p>
              <p className="text-[11px] uppercase tracking-[0.18em] text-slate-500">Compliance archive</p>
            </div>
          </div>
          <h1 className="mt-20 max-w-lg text-4xl font-semibold leading-[1.15] tracking-tight text-white">
            Every message. Encrypted, searchable, defensible.
          </h1>
          <p className="mt-5 max-w-md text-sm leading-6 text-slate-400">
            Self-hosted journaling and mailbox archiving for Microsoft 365. Search
            years of mail in milliseconds, preserve evidence under legal hold, and
            keep the keys on your own infrastructure.
          </p>
        </div>
        <div className="relative grid max-w-lg grid-cols-3 gap-3 text-xs">
          {[
            ['AES-256-GCM', 'Envelope encryption'],
            ['Row-level', 'Tenant isolation'],
            ['Immutable', 'Audit trail'],
          ].map(([title, sub]) => (
            <div key={title} className="rounded-xl border border-white/10 bg-white/[0.03] px-3 py-3">
              <p className="font-medium text-slate-200">{title}</p>
              <p className="mt-1 text-[11px] text-slate-500">{sub}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="flex items-center justify-center p-6 sm:p-10">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex items-center gap-3 lg:hidden">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-500 text-white">
              <ShieldCheck className="h-4.5 w-4.5" />
            </div>
            <span className="text-sm font-semibold text-white">PocketArchive</span>
          </div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-indigo-400">Secure sign in</p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight text-white">Welcome back</h2>
          <p className="mt-2 text-sm text-slate-400">Use your archive administrator credentials.</p>

          <form onSubmit={onSubmit} className="mt-8 space-y-4">
            <div>
              <Label htmlFor="email" className="text-slate-400">
                Email address
              </Label>
              <div className="relative">
                <Mail className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
                <Input
                  id="email"
                  type="email"
                  autoComplete="username"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="admin@yourcompany.com"
                  className="border-white/10 bg-white/[0.04] pl-9 text-white placeholder:text-slate-600 focus:border-indigo-400"
                />
              </div>
            </div>
            <div>
              <Label htmlFor="password" className="text-slate-400">
                Password
              </Label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••••••"
                  className="border-white/10 bg-white/[0.04] pl-9 text-white placeholder:text-slate-600 focus:border-indigo-400"
                />
              </div>
            </div>
            {error && (
              <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-200">
                {error}
              </div>
            )}
            <Button type="submit" size="lg" className="w-full" loading={pending}>
              Sign in to archive
              {!pending && <ArrowRight className="h-4 w-4" />}
            </Button>
          </form>

          <p className="mt-8 text-[11px] leading-5 text-slate-600">
            Sessions are stored in an HttpOnly cookie. Authentication and audit
            events are recorded by the archive engine.
          </p>
        </div>
      </section>
    </main>
  )
}
