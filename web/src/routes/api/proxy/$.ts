import { createFileRoute } from '@tanstack/react-router'

const COOKIE_NAME = 'pa_session'

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailers',
  'transfer-encoding',
  'upgrade',
  'host',
  'cookie',
])

function cookieValue(header: string | null, name: string): string | undefined {
  if (!header) return undefined
  for (const part of header.split(';')) {
    const index = part.indexOf('=')
    if (index < 0) continue
    if (part.slice(0, index).trim() === name) {
      return decodeURIComponent(part.slice(index + 1).trim())
    }
  }
  return undefined
}

function engineBaseUrl(): string {
  return (process.env.ENGINE_INTERNAL_URL ?? 'http://localhost:8080').replace(/\/$/, '')
}

async function proxy(request: Request, splat: string): Promise<Response> {
  const token = cookieValue(request.headers.get('cookie'), COOKIE_NAME)
  if (!token) {
    return Response.json(
      { error: { code: 'unauthorized', message: 'Not signed in' } },
      { status: 401 },
    )
  }

  const method = request.method.toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') {
    const origin = request.headers.get('origin')
    if (origin && origin !== new URL(request.url).origin) {
      return Response.json(
        { error: { code: 'forbidden', message: 'Cross-origin request rejected' } },
        { status: 403 },
      )
    }
  }

  const incoming = new URL(request.url)
  const target = `${engineBaseUrl()}/api/v1/${splat}${incoming.search}`
  const headers = new Headers()
  request.headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase()) && key.toLowerCase() !== 'authorization') {
      headers.set(key, value)
    }
  })
  headers.set('Authorization', `Bearer ${token}`)
  const forwarded = request.headers.get('x-forwarded-for')
  if (forwarded) headers.set('X-Forwarded-For', forwarded)

  let body: ArrayBuffer | undefined
  if (method !== 'GET' && method !== 'HEAD') {
    body = await request.arrayBuffer()
  }

  let upstream: Response
  try {
    upstream = await fetch(target, { method, headers, body, redirect: 'manual' })
  } catch {
    return Response.json(
      { error: { code: 'engine_unreachable', message: 'Archive engine is unreachable' } },
      { status: 502 },
    )
  }

  const responseHeaders = new Headers()
  upstream.headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase()) && key.toLowerCase() !== 'set-cookie') {
      responseHeaders.set(key, value)
    }
  })
  responseHeaders.set('Cache-Control', 'no-store')
  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: responseHeaders,
  })
}

export const Route = createFileRoute('/api/proxy/$')({
  server: {
    handlers: {
      GET: async ({ request, params }) => proxy(request, params._splat ?? ''),
      POST: async ({ request, params }) => proxy(request, params._splat ?? ''),
      PUT: async ({ request, params }) => proxy(request, params._splat ?? ''),
      PATCH: async ({ request, params }) => proxy(request, params._splat ?? ''),
      DELETE: async ({ request, params }) => proxy(request, params._splat ?? ''),
    },
  },
})
