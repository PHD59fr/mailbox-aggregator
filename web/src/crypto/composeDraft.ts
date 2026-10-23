
import {inlineFiles,markInline,validCID} from '../api/inlineImage'
import { base64, fromBase64 } from './encoding'
import { decryptFile, encryptFile } from './vault'
import { listStoredDrafts, replaceStoredDraft, deleteStoredDraft, getStoredDraft, pruneStoredDrafts, putStoredDraft, type StoredDraft, type StoredDraftAttachment } from './draftStorage'
import type { MailAction } from '../stores/mailActions'
import type { MessageRef } from '../api/reading'

const idStorageKey = 'mailbox-compose-draft-id-v1'
const maxDraftAge = 7 * 24 * 60 * 60 * 1000
const actions: MailAction[] = ['reply', 'reply-all', 'forward', 'forward-attachment', 'redirect','draft']

export type DraftStatus = 'idle' | 'saving' | 'saved' | 'error'

export interface DraftMeta {
  headers?:Record<string,string>
  version: 1
  action: MailAction | null
  original: MessageRef | null
  references?: string[]
  inReplyTo?: string
  smtpConfigID?: string
  to: string
  cc: string
  bcc: string
  subject: string
  text: string
  html: string
  showCc: boolean
  showBcc: boolean
  selectedSentID: string
  signatureID?: string
  signatureText?: string
  remoteDraft?:MessageRef|null
  remoteUnconfirmed?:boolean
  savedAt: number
}

export interface DraftInput {
  headers?:Record<string,string>
  action: MailAction | null
  original: MessageRef | null
  references?: string[]
  inReplyTo?: string
  smtpConfigID?: string
  to: string
  cc: string
  bcc: string
  subject: string
  text: string
  html: string
  showCc: boolean
  showBcc: boolean
  selectedSentID: string
  signatureID?: string
  signatureText?: string
  remoteDraft?:MessageRef|null
  remoteUnconfirmed?:boolean
}

export interface DraftAttachment {
  contentID?:string
  id: string
  name: string
  type: string
  lastModified: number
  size: number
  data: string
}

export interface LoadedDraft {
  meta: DraftMeta
  attachments: DraftAttachment[]
}

function newDraftID(): string {
  try {
    const existing = sessionStorage.getItem(idStorageKey)
    if (existing && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(existing)) return existing
    const id = crypto.randomUUID()
    sessionStorage.setItem(idStorageKey, id)
    return id
  } catch { return crypto.randomUUID() }
}

function metaFrom(input: DraftInput): DraftMeta {
  return {
    version: 1,headers:input.headers,
    action: input.action,
    original: input.original ? { ...input.original } : null,
    ...(input.references?.length ? { references: [...input.references] } : {}),
    ...(input.inReplyTo ? { inReplyTo: input.inReplyTo } : {}),
    ...(input.smtpConfigID ? { smtpConfigID: input.smtpConfigID } : {}),
    to: input.to, cc: input.cc, bcc: input.bcc, subject: input.subject,
    text: input.text, html: input.html, showCc: input.showCc, showBcc: input.showBcc,
    selectedSentID: input.selectedSentID, signatureID:input.signatureID,signatureText:input.signatureText,remoteDraft:input.remoteDraft,remoteUnconfirmed:input.remoteUnconfirmed,savedAt: Date.now(),
  }
}

export function hasDraftContent(meta: DraftMeta): boolean {
  return Boolean(meta.to.trim() || meta.cc.trim() || meta.bcc.trim() || meta.subject.trim() || meta.text.trim() || meta.html.trim()||Object.values(meta.headers||{}).some(Boolean))
}

function storageUnavailable(error: unknown): boolean {
  return error instanceof Error && /storage is (unavailable|blocked)/i.test(error.message)
}

function validMeta(value: unknown): value is DraftMeta {
  const meta = value as Partial<DraftMeta> | null
  return !!meta && meta.version === 1 && (meta.headers===undefined||!!meta.headers&&typeof meta.headers==='object'&&!Array.isArray(meta.headers)&&Object.entries(meta.headers).length<=3&&Object.entries(meta.headers).every(([k,v])=>['Organization','Importance','X-Priority'].includes(k)&&typeof v==='string'&&v.length<=256)) &&
    (meta.action === null || (typeof meta.action === 'string' && actions.includes(meta.action as MailAction))) &&
    (meta.original === null || (!!meta.original && typeof meta.original === 'object' && typeof meta.original.accountID === 'string' && typeof meta.original.uid === 'number' && typeof meta.original.validity === 'number')) &&
    (meta.references === undefined || Array.isArray(meta.references) && meta.references.length <= 32 && meta.references.every(id => typeof id === 'string' && id.length <= 1000 && !/[\r\n]/.test(id))) &&
    (meta.inReplyTo === undefined || typeof meta.inReplyTo === 'string') &&
    (meta.smtpConfigID === undefined || typeof meta.smtpConfigID === 'string') &&
    (meta.remoteDraft===undefined||meta.remoteDraft===null||typeof meta.remoteDraft==='object'&&typeof meta.remoteDraft.accountID==='string'&&typeof meta.remoteDraft.uid==='number'&&typeof meta.remoteDraft.validity==='number') &&
    (meta.remoteUnconfirmed===undefined||typeof meta.remoteUnconfirmed==='boolean') &&
    (meta.signatureID===undefined||typeof meta.signatureID==='string') &&
    (meta.signatureText===undefined||typeof meta.signatureText==='string') &&
    typeof meta.to === 'string' && typeof meta.cc === 'string' && typeof meta.bcc === 'string' &&
    typeof meta.subject === 'string' && typeof meta.text === 'string' && typeof meta.html === 'string' &&
    typeof meta.showCc === 'boolean' && typeof meta.showBcc === 'boolean' &&
    typeof meta.selectedSentID === 'string' && typeof meta.savedAt === 'number'
}

function validAttachment(value: unknown): value is DraftAttachment {
  const item = value as DraftAttachment | null
  return !!item && (item.contentID===undefined||typeof item.contentID==='string'&&validCID(item.contentID)) && typeof item.name === 'string' && item.name.length <= 255 &&
    typeof item.type === 'string' && item.type.length <= 127 &&
    typeof item.lastModified === 'number' && typeof item.size === 'number' &&
    typeof item.data === 'string'
}

// The draft key is derived from the VMK, so the ciphertext left in the browser
// after a lock cannot be opened until the user unlocks the vault again.
export class ComposeDraftSession {
  id = newDraftID()
  status: DraftStatus = 'idle'
  error = ''
  restoredAt = 0
  private available = typeof indexedDB !== 'undefined'
  private disposed = false
  private clearing = false
  private attachments: StoredDraftAttachment[] = []
  private queue: Promise<unknown> = Promise.resolve()
  private saveSequence = 0

  constructor(
    private readonly vmk: Uint8Array,
    private readonly userID: string,
    private readonly active: () => boolean,
    private readonly read: () => DraftInput,
  ) {}

  get enabled(): boolean { return this.available && !this.disposed }

  private fileID(kind: string): string { return `compose-draft:${this.id}:${kind}` }

  private run<T>(operation: () => Promise<T>): Promise<T> {
    const next = this.queue.then(operation, operation)
    this.queue = next.then(() => undefined, () => undefined)
    return next
  }

  async load(): Promise<LoadedDraft | null> {
    if (!this.enabled || !this.active()) return null
    try {
      const record = await getStoredDraft(this.id)
      if (!this.active() || this.disposed) return null
      if (!record) return null
      if (record.version !== 1 || !Number.isFinite(record.savedAt) || Date.now() - record.savedAt > maxDraftAge) {
        if (this.active()) await deleteStoredDraft(this.id).catch(() => {})
        return null
      }
      if (record.userID !== this.userID) return null
      const saved = await decryptFile<DraftMeta>(record.meta, this.vmk, this.userID, this.fileID('meta'))
      if (!this.active() || this.disposed) return null
      if (!validMeta(saved)) throw new Error('Invalid saved draft.')
      let legacy = false
      const sealed: StoredDraftAttachment[] = []
      const attachments = await Promise.all(record.attachments.map(async item => {
        if (!item || typeof item.id !== 'string' || item.id.length > 100 || !item.envelope) throw new Error('Invalid saved draft attachment.')
        const value = await decryptFile<unknown>(item.envelope, this.vmk, this.userID, this.fileID(`attachment:${item.id}`))
        const attachment = typeof value === 'string' ? { id: item.id, name: item.name, type: item.type, size: item.size, lastModified: item.lastModified, data: value } : value
        if (!validAttachment(attachment)) throw new Error('Invalid saved draft attachment.')
        if (typeof value === 'string') legacy = true
        const envelope = typeof value === 'string'
          ? await encryptFile(attachment, this.vmk, this.userID, this.fileID(`attachment:${item.id}`)) : item.envelope
        sealed.push({ id: item.id, envelope })
        return { ...attachment, id: item.id }
      }))
      if (!this.active() || this.disposed) return null
      this.attachments = sealed
      if (legacy) await replaceStoredDraft(record, { ...record, attachments: sealed }).catch(error => this.fail(error))
      this.restoredAt = record.savedAt
      return { meta: saved, attachments }
    } catch (error) {
      if (storageUnavailable(error)) { this.available = false; return null }
      if (!this.active() || this.disposed) return null
      await deleteStoredDraft(this.id).catch(() => {})
      this.attachments = []
      return null
    }
  }

  save(): Promise<void> {
    if (!this.enabled || this.clearing || !this.active()) return Promise.resolve()
    this.status = 'saving'
    this.error = ''
    const sequence = ++this.saveSequence
    const pending = this.run(() => this.persistLocked())
    void pending.then(
      () => {
        if (sequence !== this.saveSequence) return
        this.status = this.active() ? 'saved' : 'idle'
      },
      error => {
        if (sequence === this.saveSequence) this.fail(error)
      },
    )
    return pending
  }

  // Persist the latest composer state even if its debounced autosave has not
  // fired yet. This is what makes a lock or navigation lossless.
  async flush(): Promise<void> {
    if (!this.enabled && this.active() && (hasDraftContent(metaFrom(this.read())) || this.attachments.length)) throw new Error('Encrypted draft storage is unavailable.')
    if (this.enabled && !this.clearing && this.active()) {
      this.status = 'saving'
      this.error = ''
      const sequence = ++this.saveSequence
      try {
        await this.run(() => this.persistLocked())
        if (sequence === this.saveSequence) this.status = this.active() ? 'saved' : 'idle'
      } catch (error) {
        if (sequence === this.saveSequence) this.fail(error)
        throw error
      }
    }
    await this.queue.catch(() => {})
  }

  async addAttachment(file: File): Promise<DraftAttachment> {
    const id = crypto.randomUUID()
    const descriptor = {...(inlineFiles.has(file)?{contentID:inlineFiles.get(file)}:{}), id, name: file.name, type: file.type || 'application/octet-stream', lastModified: file.lastModified, size: file.size }
    if (!this.enabled || !this.active()) return { ...descriptor, data: '' }
    let data = ''
    try {
      await this.run(async () => {
        try { data = base64(new Uint8Array(await file.arrayBuffer())) }
        catch { throw new Error(`Could not read ${file.name}. Please attach it again.`) }
        if (!this.enabled || this.clearing || !this.active()) return
        const envelope = await encryptFile({ ...descriptor, data }, this.vmk, this.userID, this.fileID(`attachment:${id}`))
        if (!this.enabled || this.clearing || !this.active()) return
        this.attachments.push({ id, envelope })
        await this.persistLocked()
      })
    } catch (error) {
      this.fail(error)
      throw error
    }
    return { ...descriptor, data }
  }

  async removeAttachment(id: string): Promise<void> {
    const index = this.attachments.findIndex(item => item.id === id)
    if (index < 0) return
    this.attachments.splice(index, 1)
    if (!this.enabled || this.clearing) return
    try {
      await this.run(async () => {
        if (!this.active()) return
        await this.persistLocked()
      })
    } catch (error) { this.fail(error) }
  }

  async clear(): Promise<void> {
    this.clearing = true
    this.saveSequence++
    try {
      await this.queue.catch(() => {})
      if (this.available) await deleteStoredDraft(this.id).catch(() => {})
      try { if (sessionStorage.getItem(idStorageKey) === this.id) sessionStorage.removeItem(idStorageKey) } catch { /* Browser storage may be disabled. */ }
      this.attachments = []
      this.status = 'idle'
      this.error = ''
      this.restoredAt = 0
      this.id = newDraftID()
    } finally { this.clearing = false }
  }

  dispose(): void { this.disposed = true }

  private async persistLocked(): Promise<void> {
    if (!this.enabled || this.clearing || !this.active()) return
    const meta = metaFrom(this.read())
    if (!hasDraftContent(meta) && this.attachments.length === 0) {
      await deleteStoredDraft(this.id)
      return
    }
    const envelope = await encryptFile(meta, this.vmk, this.userID, this.fileID('meta'))
    if (!this.enabled || this.clearing || !this.active()) return
    const record: StoredDraft = { version: 1, id: this.id, userID: this.userID, savedAt: meta.savedAt, meta: envelope, attachments: [...this.attachments] }
    await putStoredDraft(record)
  }

  private fail(error: unknown): void {
    this.status = 'error'
    this.error = error instanceof Error ? error.message : 'The draft could not be saved.'
  }
}

export function draftAttachmentFile(attachment: DraftAttachment): File {
  const file=new File([fromBase64(attachment.data) as BlobPart], attachment.name, { type: attachment.type, lastModified: attachment.lastModified });if(attachment.contentID)markInline(file,attachment.contentID);return file
}

let activeSession: ComposeDraftSession | null = null

export function createComposeDraftSession(vmk: Uint8Array, userID: string, active: () => boolean, read: () => DraftInput): ComposeDraftSession {
  activeSession?.dispose()
  activeSession = new ComposeDraftSession(vmk, userID, active, read)
  return activeSession
}

export function flushActiveComposeDraft(): Promise<void> | null {
  return activeSession?.flush() ?? null
}

export function discardActiveComposeDraft(): Promise<void> | null {
  const current = activeSession
  activeSession = null
  return current?.clear() ?? null
}

export function disposeComposeDraftSession(session: ComposeDraftSession): void {
  session.dispose()
  if (activeSession === session) activeSession = null
}

export function pruneComposeDrafts(): Promise<void> {
  return pruneStoredDrafts(Date.now() - maxDraftAge).catch(() => {})
}

// Upgrade all this user's legacy attachment descriptors after unlocking,
// including drafts from closed tabs. Other users' records require their VMK.
export async function migrateComposeDrafts(vmk: Uint8Array, userID: string, active: () => boolean, legacyKeys: Uint8Array[] = []): Promise<void> {
  if (typeof indexedDB === 'undefined') return
  let failed=false
  for (const record of await listStoredDrafts()) {
    if (!active()) return
    if (record.userID !== userID || record.savedAt < Date.now()-maxDraftAge) continue
    try {
      let source = vmk
      let meta: DraftMeta | undefined
      for (const candidate of [vmk, ...legacyKeys]) {
        try { meta = await decryptFile<DraftMeta>(record.meta, candidate, userID, `compose-draft:${record.id}:meta`); source = candidate; break }
        catch { /* A preceding key version may own this local draft. */ }
      }
      if (!meta) throw new Error('An older draft could not be decrypted.')
      const rekey = source !== vmk
      if (!rekey && !record.attachments.some(item => item.name !== undefined)) continue
      const sealed: StoredDraftAttachment[] = []
      for (const item of record.attachments) {
        if (!active()) return
        const fileID = `compose-draft:${record.id}:attachment:${item.id}`
        const data = await decryptFile<unknown>(item.envelope, source, userID, fileID)
        const value = typeof data === 'string' ? { id: item.id, name: item.name, type: item.type, size: item.size, lastModified: item.lastModified, data } : data
        if (!validAttachment(value)) throw new Error('Invalid legacy draft attachment.')
        const envelope = await encryptFile(value, vmk, userID, fileID)
        sealed.push({ id: item.id, envelope })
      }
      const envelope = rekey ? await encryptFile(meta, vmk, userID, `compose-draft:${record.id}:meta`) : record.meta
      if (active()) await replaceStoredDraft(record, { ...record, meta: envelope, attachments: sealed })
    }catch{failed=true}
  }
  if(failed)throw new Error('Some older drafts could not be migrated. Their encrypted records were retained.')
}
