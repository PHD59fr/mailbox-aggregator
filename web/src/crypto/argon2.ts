import type { Argon2Parameters } from './vault'

export const defaultKDF: Argon2Parameters = { memory_kib: 65536, iterations: 3, parallelism: 1, key_length: 32 }
export function validateKDF(salt: Uint8Array, params: Argon2Parameters) {
  if (salt.length !== 16 || !Number.isInteger(params.memory_kib) || params.memory_kib < 65536 || params.memory_kib > 1048576 ||
    !Number.isInteger(params.iterations) || params.iterations < 2 || params.iterations > 10 ||
    !Number.isInteger(params.parallelism) || params.parallelism < 1 || params.parallelism > 8 || params.key_length !== 32) throw new Error('Unsafe Argon2id parameters')
}
let worker: Worker | null = null
let sequence = 0
const pending = new Map<number, { resolve: (value: Uint8Array) => void; reject: (error: Error) => void }>()
export function cancelKeyDerivation() {
  worker?.terminate(); worker = null
  for (const operation of pending.values()) operation.reject(new Error('Key derivation was cancelled. Unlock your vault and try again.'))
  pending.clear()
}
export async function derivePasswordKey(password: string, salt: Uint8Array, params: Argon2Parameters = defaultKDF): Promise<Uint8Array> {
  validateKDF(salt, params)
  const secret = new TextEncoder().encode(password.normalize('NFKC'))
  if (typeof globalThis.Worker !== 'function') {
    // Non-browser test runtimes and browsers without Worker support still yield
    // cooperatively instead of running a synchronous KDF on the UI thread.
    const { argon2idAsync } = await import('@noble/hashes/argon2.js')
    try { return await argon2idAsync(secret, salt, { m: params.memory_kib, t: params.iterations, p: params.parallelism, dkLen: 32, asyncTick: 10 }) }
    finally { secret.fill(0) }
  }
  if (!worker) {
    try { worker = new Worker(new URL('./argon2.worker.ts', import.meta.url), { type: 'module' }) }
    catch(error) { secret.fill(0);throw error }
    worker.onmessage = event => {
      const { id, key, error } = event.data as { id: number; key?: Uint8Array; error?: string }
      const operation = pending.get(id)
      if (!operation) { key?.fill(0); return }
      pending.delete(id)
      if (error || !(key instanceof Uint8Array) || key.length !== 32) operation.reject(new Error('Key derivation failed.'))
      else operation.resolve(key)
    }
    worker.onerror = () => cancelKeyDerivation()
  }
  const id = ++sequence
  const saltCopy = new Uint8Array(salt)
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    try { worker!.postMessage({ id, secret, salt: saltCopy, params }, [secret.buffer, saltCopy.buffer]) }
    catch (error) { pending.delete(id); secret.fill(0); reject(error) }
  })
}
