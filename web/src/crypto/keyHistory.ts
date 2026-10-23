import { api } from '../api/client'
import { decryptFile, type EncryptedFile } from './vault'
import { fromBase64 } from './encoding'
export interface LegacyKey { key: string; expires_at: number }
export interface VaultConfig { legacy_vault_keys?: LegacyKey[]; [name: string]: unknown }
export async function legacyVaultKeys(vmk: Uint8Array, userID: string): Promise<Uint8Array[]> {
  const config = await api<EncryptedFile>('/vault/files/config', { cache: 'no-store' })
  const decoded = await decryptFile<VaultConfig>(config, vmk, userID, 'config')
  const keys:Uint8Array[]=[]
  try {
    for (const value of decoded.legacy_vault_keys || []) {
      if(value.expires_at<=Date.now())continue
      const key=fromBase64(value.key)
      if(key.length!==32){key.fill(0);throw new Error('Invalid legacy vault key')}
      keys.push(key)
    }
    return keys
  }catch(error){for(const key of keys)key.fill(0);throw error}
}

