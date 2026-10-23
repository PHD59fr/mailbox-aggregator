import { argon2id } from '@noble/hashes/argon2.js'
import { validateKDF } from './argon2'
import type { Argon2Parameters } from './vault'

const scope = globalThis as unknown as { postMessage: (message: unknown, transfer?: Transferable[]) => void }

// This module receives no VMK, provider credential or session cookie.
globalThis.onmessage = event => {
  const { id, secret, salt, params } = event.data as { id: number; secret: Uint8Array; salt: Uint8Array; params: Argon2Parameters }
  try {
    validateKDF(salt, params)
    const key = argon2id(secret, salt, { m: params.memory_kib, t: params.iterations, p: params.parallelism, dkLen: 32 })
    scope.postMessage({ id, key }, [key.buffer])
  } catch { scope.postMessage({ id, error: 'Key derivation failed.' }) }
  finally { secret.fill(0); salt.fill(0) }
}
