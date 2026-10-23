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

// A single message read needs a bounded retry as well as the list pollers.
// The caller owns the abort signal so locking or changing messages cancels it.
export async function retryRead<T>(read:()=>Promise<T>,signal:AbortSignal,onRetry:()=>void=()=>{}):Promise<T>{
  for(let attempt=0;;attempt++){
    if(signal.aborted)throw new DOMException('Reading was cancelled.','AbortError')
    try{return await read()}
    catch(error){
      if(signal.aborted || attempt>=2 || !retryableRead(error))throw error
      onRetry();await waitForRetry(reconnectDelay(attempt),signal)
    }
  }
}
