import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { listAccounts } from './accounts'
import { listMessages, readMessage, listFolders, documentURL, markMessageSeen, messageSource, attachmentBlob, trashMessage } from './reading'
import { useSessionStore } from '../stores/session'

vi.mock('./accounts', () => ({ listAccounts: vi.fn() }))
const account = { id: 'work', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'imap-secret', is_default: true }
let requests: ReturnType<typeof vi.fn<typeof fetch>>
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  const session = useSessionStore(); session.accept({ user_id: 'user', state: 'AUTHENTICATED_LOCKED', csrf_token: 'csrf' }); session.vmk = new Uint8Array(32)
  vi.mocked(listAccounts).mockResolvedValue({ work: account })
  requests = vi.fn<typeof fetch>(async url => String(url).endsWith('/session/credentials') ? Response.json({}) : Response.json({ messages: [], uid_validity: 7 }))
  vi.stubGlobal('fetch', requests)
})
afterEach(() => vi.unstubAllGlobals())

describe('IMAP reading requests', () => {
  it('does not activate credentials or fall back to automatic deletion when Trash is None',async()=>{
    vi.mocked(listAccounts).mockResolvedValueOnce({work:{...account,folder_mapping:{trash:null}}})
    await expect(trashMessage({accountID:'work',uid:42,validity:7},new AbortController().signal)).rejects.toThrow('Trash is set to None')
    expect(requests).not.toHaveBeenCalled()
  })
  it('uses the encrypted account’s manual Trash choice and resets to automatic when unset',async()=>{
    vi.mocked(listAccounts).mockResolvedValueOnce({work:{...account,folder_mapping:{trash:'Deleted/été & work'}}})
    const ref={accountID:'work',uid:42,validity:7,folder:'Projects'};const signal=new AbortController().signal
    await trashMessage(ref,signal)
    expect(requests.mock.calls[requests.mock.calls.length-1]![0]).toBe('/api/v1/mailboxes/work/messages/42/trash?uid_validity=7&folder=Projects&trash_folder=Deleted%2F%C3%A9t%C3%A9%20%26%20work')
    expect(requests.mock.calls[requests.mock.calls.length-1]![1]?.method).toBe('POST')
    await trashMessage(ref,signal)
    expect(requests.mock.calls[requests.mock.calls.length-1]![0]).not.toContain('trash_folder')
    vi.mocked(listAccounts).mockResolvedValueOnce({work:{...account,folder_mapping:{sent:'Trash'}}})
    await trashMessage(ref,signal)
    expect(requests.mock.calls[requests.mock.calls.length-1]![0]).toContain('&trash_exclude=Trash')
  })
  it('sends encoded search and combined filters with folder and pagination scope',async()=>{
    await listMessages('work',new AbortController().signal,42,7,'Archive',{search:' café & "test" ',seen:'unread',from:' Team@example.test ',to:'alias+mail@example.test',subject:'Invoice & quote',since:'2026-10-01',until:'2026-10-31'})
    expect(requests.mock.calls[1]![0]).toBe('/api/v1/mailboxes/work/messages?before=42&uid_validity=7&folder=Archive&search=caf%C3%A9%20%26%20%22test%22&seen=unread&from=Team%40example.test&to=alias%2Bmail%40example.test&subject=Invoice%20%26%20quote&since=2026-10-01&until=2026-10-31')
  })
  it('encodes folder names for every message operation and document request', async () => {
    const signal = new AbortController().signal
    const ref = {accountID:'work',uid:42,validity:7,folder:'Sent/été & work'}
    await listFolders('work',signal)
    expect(requests.mock.calls[requests.mock.calls.length-1]![0]).toBe('/api/v1/mailboxes/work/folders')
    await listMessages('work',signal,0,0,ref.folder)
    expect(requests.mock.calls[requests.mock.calls.length-1]![0]).toBe('/api/v1/mailboxes/work/messages?folder=Sent%2F%C3%A9t%C3%A9%20%26%20work')
    for (const operation of [() => readMessage(ref,signal), () => markMessageSeen(ref,signal,false), () => messageSource(ref,signal), () => attachmentBlob(ref,{part:'2',filename:'a.pdf',content_type:'application/pdf',size:1},signal)]) {
      await operation(); expect(String(requests.mock.calls[requests.mock.calls.length-1]![0])).toContain('uid_validity=7&folder=Sent%2F%C3%A9t%C3%A9%20%26%20work')
    }
    expect(documentURL(ref,false)).toContain('/document?uid_validity=7&folder=Sent%2F%C3%A9t%C3%A9%20%26%20work')
  })
  it('keeps passwords out of URLs and message requests and preserves independent SMTP activation', async () => {
    useSessionStore().serverLockRevision = 7
    await listMessages('work',new AbortController().signal,42,7)
    const activation = JSON.parse(String(requests.mock.calls[0]![1]?.body))
    expect(activation.accounts.work.password).toBe('imap-secret')
    expect(activation.lock_revision).toBe(7)
    expect(activation.smtp).toBeUndefined()
    expect(requests.mock.calls[1]![0]).toBe('/api/v1/mailboxes/work/messages?before=42&uid_validity=7')
    expect(requests.mock.calls[1]![1]?.body).toBeUndefined()
    expect(new Headers(requests.mock.calls[0]![1]?.headers).get('X-CSRF-Token')).toBe('csrf')
  })
  it('never requests message content if locked during activation', async () => {
    requests.mockImplementation(async () => { useSessionStore().lockLocal(); return Response.json({}) })
    await expect(readMessage({ accountID: 'work', uid: 42, validity: 7 },new AbortController().signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(requests).toHaveBeenCalledTimes(1)
  })
  it('discards content returned after locking', async () => {
    requests.mockImplementation(async url => {
      if (String(url).includes('/messages/')) useSessionStore().lockLocal()
      return Response.json({ text: 'private message' })
    })
    await expect(readMessage({ accountID: 'work', uid: 42, validity: 7 },new AbortController().signal)).rejects.toMatchObject({ name: 'AbortError' })
  })
  it('does not attempt empty-password or aborted logins', async () => {
    vi.mocked(listAccounts).mockResolvedValue({ work: { ...account, password: '' } })
    await expect(listMessages('work',new AbortController().signal)).rejects.toThrow('IMAP password')
    const controller = new AbortController(); controller.abort()
    await expect(listMessages('work',controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(requests).not.toHaveBeenCalled()
  })
})
