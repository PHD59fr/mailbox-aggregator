import { encryptFile } from './vault'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { migrateComposeDrafts, createComposeDraftSession, disposeComposeDraftSession, hasDraftContent, type ComposeDraftSession, type DraftInput } from './composeDraft'

const draftStore = vi.hoisted(() => ({ records: new Map<string, unknown>() }))

vi.mock('./draftStorage', () => ({
  listStoredDrafts: async () => [...draftStore.records.values()],
  replaceStoredDraft: async (previous: unknown, record: unknown) => { const id=(record as {id:string}).id; if(JSON.stringify(draftStore.records.get(id))===JSON.stringify(previous)) draftStore.records.set(id,record) },
  getStoredDraft: async (id: string) => draftStore.records.get(id),
  putStoredDraft: async (record: unknown) => { draftStore.records.set((record as { id: string }).id, record) },
  deleteStoredDraft: async (id: string) => { draftStore.records.delete(id) },
  pruneStoredDrafts: async (before: number) => {
    for (const [id, value] of draftStore.records) {
      if ((value as { savedAt: number }).savedAt < before) draftStore.records.delete(id)
    }
  },
}))

function draft(overrides: Partial<DraftInput> = {}): DraftInput {
  return {
    action: 'reply',
    original: { accountID: 'imap-work', uid: 4, validity: 7, folder: 'INBOX' },
    inReplyTo: '<source@example.test>',
    smtpConfigID: 'sender-id',
    to: 'writer@example.test',
    cc: 'copy@example.test',
    bcc: 'hidden@example.test',
    subject: 'Quarterly secret',
    text: 'Private body',
    html: '<p>Private body</p>',
    showCc: true,
    showBcc: true,
    selectedSentID: 'sent-account',
    ...overrides,
  }
}

let sessions: ComposeDraftSession[] = []

function create(vmk: Uint8Array, userID: string, active: () => boolean, read: () => DraftInput) {
  const session = createComposeDraftSession(vmk, userID, active, read)
  sessions.push(session)
  return session
}

beforeEach(() => {
  draftStore.records.clear()
  sessionStorage.clear()
  vi.stubGlobal('indexedDB', {})
})

afterEach(() => {
  for (const session of sessions) disposeComposeDraftSession(session)
  sessions = []
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('encrypted compose drafts', () => {
  it('retains an unreadable record while rotating the other healthy drafts',async()=>{
    const oldKey=new Uint8Array(32).fill(7);const nextKey=new Uint8Array(32).fill(8)
    const badKey=new Uint8Array(32).fill(9)
    const broken=create(badKey,'user',()=>true,()=>draft({subject:'Unrelated inaccessible draft'}))
    await broken.save();broken.dispose()
    sessionStorage.clear()
    const healthy=create(oldKey,'user',()=>true,()=>draft({subject:'Preserve this draft'}))
    await healthy.save();healthy.dispose()
    const before=JSON.stringify(draftStore.records.get(broken.id))
    await expect(migrateComposeDrafts(nextKey,'user',()=>true,[oldKey])).rejects.toThrow('retained')
    expect(JSON.stringify(draftStore.records.get(broken.id))).toBe(before)
    const restored=create(nextKey,'user',()=>true,()=>draft())
    expect((await restored.load())?.meta.subject).toBe('Preserve this draft')
  })
  it('persists only ciphertext and restores fields and attachments in a new session', async () => {
    const vmk = new Uint8Array(32).fill(9)
    const current = draft()
    const session = create(vmk, 'user', () => true, () => current)
    await session.save()

    expect(draftStore.records.size).toBe(1)
    const envelope = JSON.stringify([...draftStore.records.values()])
    for (const plaintext of ['Quarterly secret', 'Private body', 'writer@example.test', 'copy@example.test', 'hidden@example.test']) {
      expect(envelope).not.toContain(plaintext)
    }

    const attachment = await session.addAttachment(new File(['attachment secret'], 'notes.txt', { type: 'text/plain' }))
    expect(attachment.data).toBe('YXR0YWNobWVudCBzZWNyZXQ')
    const withAttachment = JSON.stringify([...draftStore.records.values()])
    expect(withAttachment).not.toContain('attachment secret')
    expect(withAttachment).not.toContain('YXR0YWNobWVudCBzZWNyZXQ')

    session.dispose()
    const restored = create(vmk, 'user', () => true, () => draft())
    const loaded = await restored.load()
    expect(loaded?.meta).toMatchObject({
      action: 'reply',
      to: 'writer@example.test',
      cc: 'copy@example.test',
      bcc: 'hidden@example.test',
      subject: 'Quarterly secret',
      text: 'Private body',
      html: '<p>Private body</p>',
      showCc: true,
      showBcc: true,
      selectedSentID: 'sent-account',
      inReplyTo: '<source@example.test>',
      smtpConfigID: 'sender-id',
    })
    expect(loaded?.meta.original).toEqual({ accountID: 'imap-work', uid: 4, validity: 7, folder: 'INBOX' })
    expect(loaded?.attachments).toHaveLength(1)
    expect(loaded?.attachments[0]).toMatchObject({ id: attachment.id, name: 'notes.txt', type: 'text/plain', data: attachment.data })
  })

  it('flushes the latest composer state and does not overwrite a draft after the vault locks', async () => {
    const vmk = new Uint8Array(32).fill(5)
    let active = true
    const current = draft({ subject: 'First version' })
    const session = create(vmk, 'user', () => active, () => current)
    await session.save()

    current.subject = 'Latest version'
    await session.flush()
    session.dispose()
    const reloaded = create(vmk, 'user', () => true, () => draft())
    expect((await reloaded.load())?.meta.subject).toBe('Latest version')

    const locked = create(vmk, 'user', () => active, () => current)
    await locked.save()
    current.subject = 'Must not be persisted'
    active = false
    await locked.flush()
    locked.dispose()
    const afterLock = create(vmk, 'user', () => true, () => draft())
    expect((await afterLock.load())?.meta.subject).toBe('Latest version')
  })

  it('deletes ciphertext on discard and starts a new draft identity', async () => {
    const vmk = new Uint8Array(32).fill(3)
    const session = create(vmk, 'user', () => true, () => draft())
    await session.save()
    const previousID = session.id
    expect(draftStore.records.size).toBe(1)

    await session.clear()
    expect(draftStore.records.size).toBe(0)
    expect(session.id).not.toBe(previousID)
    expect(sessionStorage.getItem('mailbox-compose-draft-id-v1')).toBe(session.id)
  })

  it('refuses to decrypt with another vault key or an expired record', async () => {
    const vmk = new Uint8Array(32).fill(1)
    const session = create(vmk, 'user', () => true, () => draft())
    await session.save()
    session.dispose()

    const wrongKey = create(new Uint8Array(32).fill(2), 'user', () => true, () => draft())
    expect(await wrongKey.load()).toBeNull()
    expect(draftStore.records.size).toBe(0)

    const fresh = create(vmk, 'user', () => true, () => draft())
    await fresh.save()
    fresh.dispose()
    const stored = [...draftStore.records.values()][0] as { savedAt: number }
    stored.savedAt = Date.now() - 8 * 24 * 60 * 60 * 1000
    const expired = create(vmk, 'user', () => true, () => draft())
    expect(await expired.load()).toBeNull()
    expect(draftStore.records.size).toBe(0)
  })

  it('treats a draft with no recipient, subject, body or attachment as empty', () => {
    expect(hasDraftContent({ version: 1, action: null, original: null, to: '', cc: '', bcc: '', subject: '', text: '', html: '', showCc: false, showBcc: false, selectedSentID: '', savedAt: 0 })).toBe(false)
    expect(hasDraftContent({ version: 1, action: null, original: null, to: 'to@example.test', cc: '', bcc: '', subject: '', text: '', html: '', showCc: false, showBcc: false, selectedSentID: '', savedAt: 0 })).toBe(true)
  })
})

it('regression: attachment filenames must remain encrypted in browser storage',async()=>{
 const session=create(new Uint8Array(32).fill(3),'user',()=>true,()=>draft())
 await session.addAttachment(new File(['private payload'],'Confidentiel-licenciement-Alice.pdf',{type:'application/pdf'}))
 expect(JSON.stringify([...draftStore.records.values()])).not.toContain('Confidentiel-licenciement-Alice.pdf')
})

it('migrates legacy attachment descriptors across closed-tab drafts after unlock without touching other users',async()=>{
 const vmk=new Uint8Array(32).fill(4)
 const session=create(vmk,'user',()=>true,()=>draft())
 await session.save()
 const envelope=await encryptFile('c2VjcmV0',vmk,'user',`compose-draft:${session.id}:attachment:legacy`)
 const previous=draftStore.records.get(session.id) as {attachments:unknown[]}
 previous.attachments=[{id:'legacy',name:'Confidential.pdf',type:'application/pdf',size:6,lastModified:1,envelope}]
 draftStore.records.set('other-user',{id:'other-user',userID:'other',attachments:[{name:'other.pdf'}]})
 await migrateComposeDrafts(vmk,'user',()=>true)
 expect(JSON.stringify(draftStore.records.get(session.id))).not.toContain('Confidential.pdf')
 expect(JSON.stringify(draftStore.records.get('other-user'))).toContain('other.pdf')
 const restored=await session.load()
 expect(restored?.attachments[0]).toMatchObject({name:'Confidential.pdf',data:'c2VjcmV0'})
})
