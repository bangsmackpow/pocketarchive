export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

interface ErrorEnvelope {
  error?: { code?: string; message?: string }
}

export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(`/api/proxy${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...init.headers,
    },
  })
  if (res.status === 401 && typeof window !== 'undefined') {
    window.location.assign('/login')
  }
  const contentType = res.headers.get('content-type') ?? ''
  const payload = contentType.includes('application/json')
    ? await res.json().catch(() => null)
    : await res.text().catch(() => '')
  if (!res.ok) {
    const env = (payload ?? {}) as ErrorEnvelope
    throw new ApiError(
      res.status,
      env.error?.code ?? 'request_failed',
      env.error?.message ??
        (typeof payload === 'string' && payload
          ? payload
          : `Request failed with status ${res.status}`),
    )
  }
  return payload as T
}

export function downloadUrl(path: string): string {
  return `/api/proxy${path}`
}

export async function uploadChunk(
  path: string,
  chunk: Blob,
  onProgress: (loaded: number) => void,
  signal?: AbortSignal,
): Promise<{ bytes_received: number; chunks_received: number }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', `/api/proxy${path}`)
    xhr.responseType = 'json'
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded)
    }
    xhr.onerror = () => reject(new Error('Network error while uploading chunk'))
    xhr.onabort = () => reject(new DOMException('Upload cancelled', 'AbortError'))
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response as { bytes_received: number; chunks_received: number })
        return
      }
      const message =
        (xhr.response as { error?: { message?: string } } | null)?.error
          ?.message ?? `Chunk upload failed with status ${xhr.status}`
      reject(new ApiError(xhr.status, 'chunk_failed', message))
    }
    if (signal) {
      signal.addEventListener('abort', () => xhr.abort(), { once: true })
    }
    xhr.send(chunk)
  })
}
