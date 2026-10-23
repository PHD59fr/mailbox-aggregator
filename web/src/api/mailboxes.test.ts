import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { listAccounts } from './accounts'
import { watchMailboxes } from './mailboxes'
import { useSessionStore } from '../stores/session'

vi.mock('./accounts', () => ({ listAccounts: vi.fn() }))
const account = { id: 'receiver', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'imap-secret', is_default: true }
let stream: ReadableStreamDefaultController<Uint8Array>
let requests: ReturnType<typeof vi.fn<typeof fetch>>
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  const session = useSessionStore()
  session.accept({ user_id: 'test-user', state: 'AUTHENTICATED_LOCKED', csrf_token: 'test-csrf' })
  session.vmk = new Uint8Array(32).fill(7)
  vi.mocked(listAccounts).mockResolvedValue({ receiver: account })
  requests = vi.fn<typeof fetch>(async (url, init) => {
    if (url === '/api/v1/session/credentials') return Response.json({ state: 'AUTHENTICATED_UNLOCKED' })
    if (url === '/api/v1/mailboxes/presence') return new Response(null, { status: 204 })
    if (url === '/api/v1/mailboxes/watch') {
      expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('test-csrf')
      return new Response(new ReadableStream<Uint8Array>({ start(controller) { stream = controller } }), { headers: { 'X-Watch-ID': 'watch-test' } })
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  vi.stubGlobal('fetch', requests)
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

describe('browser mailbox streaming', () => {
  it('activates receiving alone and decodes updates split across chunks', async () => {
    useSessionStore().serverLockRevision = 7
    const controller = new AbortController(); const receive = vi.fn()
    const watching = watchMailboxes(60, controller.signal, receive)
    await vi.waitFor(() => expect(requests).toHaveBeenCalledWith('/api/v1/mailboxes/watch', expect.anything()))
    const encoder = new TextEncoder()
    stream.enqueue(encoder.encode('{"type":"status","account_id":"receiver","mode":"idle","un'))
    stream.enqueue(encoder.encode('seen":2,"messages":3,"uid_next":4,"uid_validity":1}\n'))
    stream.close(); await watching
    expect(receive).toHaveBeenCalledWith(expect.objectContaining({ account_id: 'receiver', unseen: 2, mode: 'idle' }))
    const activation = JSON.parse(String(requests.mock.calls[0]![1]?.body))
    expect(activation.accounts.receiver.password).toBe(account.password)
    expect(activation.lock_revision).toBe(7)
    expect(activation.smtp).toBeUndefined()
    const watchRequest = String(requests.mock.calls[1]![1]?.body)
    expect(watchRequest).toContain('60')
    expect(watchRequest).not.toContain(account.password)
  })

  it('sends presence and stops immediately when the tab aborts its connection', async () => {
    const controller = new AbortController()
    const watching = watchMailboxes(30, controller.signal, vi.fn())
    await vi.waitFor(() => expect(requests).toHaveBeenCalledTimes(2))
    // Install fake timers before starting a fresh stream so the presence timer is controlled.
    controller.abort(); await watching
    vi.useFakeTimers()
    const next = new AbortController()
    const second = watchMailboxes(30, next.signal, vi.fn())
    await vi.advanceTimersByTimeAsync(0)
    await vi.advanceTimersByTimeAsync(15000)
    expect(requests.mock.calls.some(([url]) => url === '/api/v1/mailboxes/presence')).toBe(true)
    next.abort(); await second
    const count = requests.mock.calls.length
    await vi.advanceTimersByTimeAsync(60000)
    expect(requests).toHaveBeenCalledTimes(count)
  })

  it('does not activate credentials when the vault locks during config loading', async () => {
    vi.mocked(listAccounts).mockImplementation(async () => { useSessionStore().lockLocal(); return { receiver: account } })
    await watchMailboxes(30, new AbortController().signal, vi.fn())
    expect(requests).not.toHaveBeenCalled()
  })

  it('asks for an IMAP password instead of attempting an empty login', async () => {
    vi.mocked(listAccounts).mockResolvedValue({ receiver: { ...account, password: undefined } })
    const receive = vi.fn()
    await watchMailboxes(30, new AbortController().signal, receive)
    expect(receive).toHaveBeenCalledWith(expect.objectContaining({ error: 'Add an IMAP password in Settings.' }))
    expect(requests).not.toHaveBeenCalled()
  })
})

it('regression: partial activation errors must be reported for the failed account',async()=>{
 vi.mocked(listAccounts).mockResolvedValue({receiver:account,bad:{...account,id:'bad',imap_host:'bad.invalid'}})
 requests.mockImplementation(async(url)=>{
  if(url==='/api/v1/session/credentials')return Response.json({account_errors:{bad:'Mail server could not be resolved.'}})
  if(url==='/api/v1/mailboxes/watch')return new Response(new ReadableStream({start(c){c.close()}}),{headers:{'X-Watch-ID':'id'}})
  throw new Error(String(url))
 })
 const receive=vi.fn();await watchMailboxes(30,new AbortController().signal,receive)
 expect(receive).toHaveBeenCalledWith(expect.objectContaining({account_id:'bad',error:expect.any(String)}))
})
