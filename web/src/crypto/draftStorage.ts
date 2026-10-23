import type { EncryptedFile } from './vault'

// Attachment names, types, sizes and timestamps are encrypted with the content.
// Optional descriptors exist only to migrate drafts written by the old format.
export interface StoredDraftAttachment {
  id: string
  name?: string
  type?: string
  lastModified?: number
  size?: number
  envelope: EncryptedFile
}

export interface StoredDraft {
  version: 1
  id: string
  userID: string
  savedAt: number
  meta: EncryptedFile
  attachments: StoredDraftAttachment[]
}

const databaseName = 'mailbox-compose-draft-v1'
const storeName = 'drafts'

async function database(): Promise<IDBDatabase> {
  if (typeof indexedDB === 'undefined') throw new Error('Encrypted draft storage is unavailable.')
  return new Promise((resolve, reject) => {
    let blocked = false
    const request = indexedDB.open(databaseName, 1)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(storeName)) request.result.createObjectStore(storeName, { keyPath: 'id' })
    }
    request.onerror = () => reject(request.error)
    request.onblocked = () => { blocked = true; reject(new Error('Encrypted draft storage is blocked.')) }
    request.onsuccess = () => { if (blocked) request.result.close(); else resolve(request.result) }
  })
}

async function run<T>(mode: IDBTransactionMode, operation: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const db = await database()
  try {
    return await new Promise<T>((resolve, reject) => {
      const transaction = db.transaction(storeName, mode)
      const request = operation(transaction.objectStore(storeName))
      transaction.oncomplete = () => resolve(request.result)
      transaction.onabort = transaction.onerror = () => reject(transaction.error || new Error('Encrypted draft storage failed.'))
    })
  } finally { db.close() }
}

export async function getStoredDraft(id: string): Promise<StoredDraft | undefined> {
  return run<StoredDraft | undefined>('readonly', store => store.get(id))
}

export async function putStoredDraft(record: StoredDraft): Promise<void> {
  await run<IDBValidKey>('readwrite', store => store.put(record))
}

export async function deleteStoredDraft(id: string): Promise<void> {
  await run<undefined>('readwrite', store => store.delete(id))
}

export async function pruneStoredDrafts(before: number): Promise<void> {
  if (typeof indexedDB === 'undefined') return
  const db = await database()
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(storeName, 'readwrite')
      const cursor = transaction.objectStore(storeName).openCursor()
      cursor.onsuccess = () => {
        const row = cursor.result
        if (!row) return
        if ((row.value as StoredDraft).savedAt < before) row.delete()
        row.continue()
      }
      transaction.oncomplete = () => resolve()
      transaction.onerror = transaction.onabort = () => reject(transaction.error)
    })
  } finally { db.close() }
}

export async function listStoredDrafts(): Promise<StoredDraft[]> {
  return run<StoredDraft[]>('readonly', store => store.getAll())
}

// Background migration may race with an active composer's autosave. Commit
// only if the persisted record is still the version which was decrypted.
export async function replaceStoredDraft(previous: StoredDraft, next: StoredDraft): Promise<void> {
  const db = await database()
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = db.transaction(storeName, 'readwrite')
      const store = transaction.objectStore(storeName)
      const request = store.get(previous.id)
      request.onsuccess = () => {
        if (JSON.stringify(request.result) === JSON.stringify(previous)) store.put(next)
      }
      transaction.oncomplete = () => resolve()
      transaction.onerror = transaction.onabort = () => reject(transaction.error)
    })
  } finally { db.close() }
}
