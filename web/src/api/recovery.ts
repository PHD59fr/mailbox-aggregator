import { APIError } from './client'

// For idempotent mailbox reads only. Never wrap SMTP submissions or probes.
export function retryableRead(error: unknown) {
  return error instanceof TypeError || (error instanceof APIError &&
    ([408, 429, 502, 503, 504].includes(error.status) || error.status === 0))
}
export const reconnectDelay = (attempt: number) => Math.min(30000, 3000 * 2 ** Math.min(attempt, 4))

export function waitForRetry(delay: number, signal: AbortSignal) {
  return new Promise<void>(resolve => {
    if (signal.aborted) { resolve(); return }
    const finish = () => { clearTimeout(timer); signal.removeEventListener('abort', finish); resolve() }
    const timer = setTimeout(finish, delay)
    signal.addEventListener('abort', finish, { once: true })
  })
}
