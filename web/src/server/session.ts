import { createServerFn } from '@tanstack/react-start'
import { deleteCookie, getCookie, setCookie } from '@tanstack/react-start/server'
import type { Session } from '~/lib/types'

const COOKIE_NAME = 'pa_session'

function engineUrl(): string {
  return (process.env.ENGINE_INTERNAL_URL ?? 'http://localhost:8080').replace(/\/$/, '')
}

function cookieSecure(): boolean {
  if (process.env.SESSION_COOKIE_SECURE === 'false') return false
  return process.env.NODE_ENV === 'production'
}

export const getSessionFn = createServerFn({ method: 'GET' }).handler(
  async (): Promise<Session | null> => {
    const token = getCookie(COOKIE_NAME)
    if (!token) return null
    try {
      const res = await fetch(`${engineUrl()}/api/v1/auth/me`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) return null
      return (await res.json()) as Session
    } catch {
      return null
    }
  },
)

export const loginFn = createServerFn({ method: 'POST' })
  .validator((data: { email: string; password: string }) => data)
  .handler(async ({ data }): Promise<{ ok: boolean; error?: string }> => {
    const email = data.email.trim()
    if (!email || !data.password) {
      return { ok: false, error: 'Email and password are required.' }
    }
    let res: Response
    try {
      res = await fetch(`${engineUrl()}/api/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password: data.password }),
      })
    } catch {
      return { ok: false, error: 'Archive engine is unreachable. Check the deployment.' }
    }
    const payload = (await res.json().catch(() => null)) as
      | { token?: string; expires_at?: string; error?: { message?: string } }
      | null
    if (!res.ok || !payload?.token) {
      return {
        ok: false,
        error: payload?.error?.message ?? 'Invalid email or password.',
      }
    }
    const maxAge = payload.expires_at
      ? Math.max(60, Math.floor((new Date(payload.expires_at).getTime() - Date.now()) / 1000))
      : 12 * 60 * 60
    setCookie(COOKIE_NAME, payload.token, {
      httpOnly: true,
      sameSite: 'lax',
      secure: cookieSecure(),
      path: '/',
      maxAge,
    })
    return { ok: true }
  })

export const logoutFn = createServerFn({ method: 'POST' }).handler(async () => {
  const token = getCookie(COOKIE_NAME)
  if (token) {
    try {
      await fetch(`${engineUrl()}/api/v1/auth/logout`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
    } catch {
      // Local cookie removal is still correct if the engine is down.
    }
  }
  deleteCookie(COOKIE_NAME, { path: '/' })
  return { ok: true }
})
