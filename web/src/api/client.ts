export class APIError extends Error { constructor(public code: string, message: string, public status: number, public cause?: unknown) { super(message) } }

let csrfToken = ''
export function setCSRFToken(value: string): void { csrfToken = value }

// Compression proxies can weaken or replace HTTP ETags. Keep the file's
// optimistic-lock token separate, with ETag fallback for older servers.
export function resourceRevision(response: Response): string | null {
  return response.headers.get('X-Resource-Revision') || response.headers.get('ETag')
}

export async function apiResponse(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (init.method && !['GET', 'HEAD'].includes(init.method.toUpperCase()) && csrfToken) headers.set('X-CSRF-Token', csrfToken)
  const response = await fetch(`/api/v1${path}`, { ...init, headers, credentials: 'same-origin' })
  if (!response.ok) { const data = await response.json().catch(() => null) as { error?: { code?: string; message?: string } } | null; throw new APIError(data?.error?.code ?? 'REQUEST_FAILED', data?.error?.message ?? 'The request failed.', response.status) }
  return response
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await apiResponse(path, init)
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
