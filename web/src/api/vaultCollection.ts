import { api, apiResponse, resourceRevision } from './client'
import { decryptFile, encryptFile, type EncryptedFile } from '../crypto/vault'
import { useSessionStore } from '../stores/session'

type CollectionName = 'accounts' | 'smtp' | 'preferences'

export async function readCollection<T extends { id: string }>(name: CollectionName) {
  const session = useSessionStore()
  const { userID, vmk } = session
  if (!session.authenticated || !vmk) throw new Error('Unlock your vault first.')
  const assertUnlocked = () => {
    if (!session.authenticated || session.userID !== userID || session.vmk !== vmk) {
      throw new Error('The vault was locked. Unlock it and try again.')
    }
  }
  const response = await apiResponse(`/vault/files/${name}`, { cache: 'no-store' })
  const revision = resourceRevision(response)
  const encrypted = await response.json() as EncryptedFile
  assertUnlocked()
  const data = await decryptFile<Record<string, T> | T[]>(encrypted, vmk, userID, name)
  assertUnlocked()
  if (!data || typeof data !== 'object') throw new Error('Invalid vault collection.')
  const records: Record<string, T> = Array.isArray(data)
    ? Object.fromEntries(data.map(record => [record.id, record]))
    : data

  return {
    records,
    revision,
    assertUnlocked,
    async save() {
      assertUnlocked()
      if (!revision) throw new Error('Missing vault revision. Reload and try again.')
      const envelope = await encryptFile(records, vmk, userID, name, encrypted.key_version)
      assertUnlocked()
      await api<void>(`/vault/files/${name}`, {
        method: 'PUT',
        headers: { 'If-Match': revision },
        body: JSON.stringify(envelope),
      })
    },
  }
}
