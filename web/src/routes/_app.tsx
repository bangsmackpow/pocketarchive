import * as React from 'react'
import {
  Link,
  Outlet,
  createFileRoute,
  redirect,
  useLocation,
  useRouter,
} from '@tanstack/react-router'
import { useServerFn } from '@tanstack/react-start'
import {
  Archive,
  ChevronDown,
  LogOut,
  Menu,
  Scale,
  Search,
  Server,
  ShieldCheck,
  UploadCloud,
  X,
} from 'lucide-react'
import { getSessionFn, logoutFn } from '~/server/session'
import { cn, initials } from '~/lib/format'
import { Badge } from '~/components/ui'

export const Route = createFileRoute('/_app')({
  beforeLoad: async ({ location }) => {
    const session = await getSessionFn()
    if (!session) {
      throw redirect({ to: '/login', search: { redirect: location.href } })
    }
    return session
  },
  component: AppLayout,
})

const NAV = [
  { to: '/', label: 'Archive search', icon: Search, exact: true },
  { to: '/imports', label: 'Imports', icon: UploadCloud },
  { to: '/sources', label: 'Sources', icon: Server },
  { to: '/compliance', label: 'Compliance', icon: Scale },
] as const

const TITLES: Record<string, string> = {
  '/': 'Archive search',
  '/imports': 'Import jobs',
  '/sources': 'Ingestion sources',
  '/compliance': 'Compliance & audit',
}

function AppLayout() {
  const session = Route.useRouteContext()
  const router = useRouter()
  const location = useLocation()
  const logout = useServerFn(logoutFn)
  const [mobileNav, setMobileNav] = React.useState(false)
  const [menuOpen, setMenuOpen] = React.useState(false)

  React.useEffect(() => {
    setMobileNav(false)
    setMenuOpen(false)
  }, [location.pathname])

  async function onLogout() {
    await logout()
    await router.invalidate()
    await router.navigate({ to: '/login' })
  }

  const title = TITLES[location.pathname] ?? 'PocketArchive'

  return (
    <div className="min-h-screen bg-slate-100">
      {mobileNav && (
        <button
          className="fixed inset-0 z-30 bg-slate-950/40 backdrop-blur-[1px] lg:hidden"
          aria-label="Close navigation"
          onClick={() => setMobileNav(false)}
        />
      )}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-slate-800/60 bg-slate-950 transition-transform duration-200 lg:translate-x-0',
          mobileNav ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex h-16 items-center justify-between px-5">
          <Link to="/" className="flex items-center gap-2.5">
            <div className="flex h-8.5 w-8.5 items-center justify-center rounded-lg bg-indigo-500 text-white shadow-lg shadow-indigo-500/20">
              <ShieldCheck className="h-4.5 w-4.5" />
            </div>
            <div>
              <p className="text-sm font-semibold tracking-tight text-white">PocketArchive</p>
              <p className="text-[10px] uppercase tracking-[0.16em] text-slate-500">Compliance archive</p>
            </div>
          </Link>
          <button
            className="rounded-md p-1 text-slate-400 hover:bg-white/5 lg:hidden"
            onClick={() => setMobileNav(false)}
            aria-label="Close navigation"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <nav className="mt-4 flex-1 space-y-1 px-3">
          <p className="px-2 pb-2 text-[10px] font-semibold uppercase tracking-[0.18em] text-slate-600">
            Workspace
          </p>
          {NAV.map((item) => {
            const Icon = item.icon
            const active = 'exact' in item && item.exact
              ? location.pathname === item.to
              : location.pathname.startsWith(item.to)
            return (
              <Link
                key={item.to}
                to={item.to}
                className={cn(
                  'group flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition',
                  active
                    ? 'bg-white/[0.08] font-medium text-white'
                    : 'text-slate-400 hover:bg-white/[0.04] hover:text-slate-200',
                )}
              >
                <Icon
                  className={cn(
                    'h-4 w-4',
                    active ? 'text-indigo-400' : 'text-slate-500 group-hover:text-slate-300',
                  )}
                />
                {item.label}
              </Link>
            )
          })}
        </nav>

        <div className="m-3 rounded-xl border border-white/5 bg-white/[0.03] p-3">
          <div className="flex items-center gap-2 text-xs font-medium text-slate-300">
            <Archive className="h-3.5 w-3.5 text-indigo-400" />
            Tenant
          </div>
          <p className="mt-1.5 truncate text-xs text-slate-500">{session.tenant.name}</p>
          <p className="mt-0.5 font-mono text-[10px] text-slate-600">{session.tenant.slug}</p>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-slate-200/80 bg-white/85 px-4 backdrop-blur-md sm:px-6">
          <div className="flex items-center gap-3">
            <button
              className="rounded-lg p-2 text-slate-600 hover:bg-slate-100 lg:hidden"
              onClick={() => setMobileNav(true)}
              aria-label="Open navigation"
            >
              <Menu className="h-4.5 w-4.5" />
            </button>
            <div>
              <h1 className="text-sm font-semibold tracking-tight text-slate-900">{title}</h1>
              <p className="hidden text-[11px] text-slate-500 sm:block">
                Encrypted, immutable email archive
              </p>
            </div>
          </div>

          <div className="relative">
            <button
              onClick={() => setMenuOpen((v) => !v)}
              className="focus-ring flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-slate-100"
            >
              <span className="flex h-7 w-7 items-center justify-center rounded-full bg-slate-900 text-[10px] font-semibold text-white">
                {initials(session.user.email)}
              </span>
              <span className="hidden text-left sm:block">
                <span className="block max-w-40 truncate text-xs font-medium text-slate-800">
                  {session.user.email}
                </span>
                <span className="block text-[10px] uppercase tracking-wide text-slate-500">
                  {session.user.role}
                </span>
              </span>
              <ChevronDown className="hidden h-3.5 w-3.5 text-slate-400 sm:block" />
            </button>
            {menuOpen && (
              <div className="absolute right-0 mt-2 w-64 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl">
                <div className="border-b border-slate-100 px-4 py-3">
                  <p className="truncate text-xs font-medium text-slate-800">{session.user.email}</p>
                  <div className="mt-1.5 flex items-center gap-1.5">
                    <Badge tone={session.user.role === 'admin' ? 'indigo' : 'neutral'}>
                      {session.user.role}
                    </Badge>
                    <span className="text-[10px] text-slate-500">{session.tenant.name}</span>
                  </div>
                </div>
                <button
                  onClick={onLogout}
                  className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs text-slate-600 hover:bg-slate-50"
                >
                  <LogOut className="h-3.5 w-3.5" />
                  Sign out
                </button>
              </div>
            )}
          </div>
        </header>

        <main className="p-4 sm:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
