import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { activateCredentials, resetCredentials } from './credentials'
import { useSessionStore } from '../stores/session'

const input = { lifetime_seconds: 300, accounts: {
  own: { host: 'imap.example.test', port: 993, security: 'tls' as const, username: 'own', password: 'synthetic' },
} }
let requests: ReturnType<typeof vi.fn<typeof fetch>>

beforeEach(() => {
  setActivePinia(createPinia())
  const session = useSessionStore()
  session.accept({ user_id: 'user', csrf_token: 'csrf', state: 'AUTHENTICATED_LOCKED', lock_revision: 4 })
  session.$patch({ vmk: new Uint8Array(32).fill(7), resumeOnReload: false })
  requests = vi.fn<typeof fetch>().mockResolvedValue(Response.json({}))
  vi.stubGlobal('fetch', requests)
})
afterEach(() => vi.unstubAllGlobals())

describe('provider activation and explicit lock revisions', () => {
  it('binds activation to the revision known by this unlocked tab', async () => {
    await activateCredentials(input)
    expect(JSON.parse(String(requests.mock.calls[0]![1]?.body))).toEqual({ ...input, mode: 'merge', lock_revision: 4 })
    expect(new Headers(requests.mock.calls[0]![1]?.headers).get('X-CSRF-Token')).toBe('csrf')
  })

  it('clears this tab if another tab locked the server session', async () => {
    const session = useSessionStore()
    const key = session.vmk!
    requests.mockResolvedValueOnce(Response.json({ error: { code: 'LOCK_REVISION_CONFLICT', message: 'Unlock again.' } }, { status: 409 }))
    await expect(activateCredentials(input)).rejects.toMatchObject({ code: 'LOCK_REVISION_CONFLICT' })
    expect(session.unlocked).toBe(false)
    expect(key.every(byte => byte === 0)).toBe(true)
    expect(requests).toHaveBeenCalledTimes(1)
  })

  it('does not let an old conflict clear a vault that has since been unlocked again', async () => {
    let respond!: (response: Response) => void
    requests.mockImplementationOnce(() => new Promise(resolve => { respond = resolve }))
    const activation = activateCredentials(input)
    const session = useSessionStore()
    session.lockLocal(false)
    session.$patch({ vmk: new Uint8Array(32).fill(8), serverLockRevision: 5 })
    respond(Response.json({ error: { code: 'LOCK_REVISION_CONFLICT' } }, { status: 409 }))
    await expect(activation).rejects.toMatchObject({ code: 'LOCK_REVISION_CONFLICT' })
    expect(session.vmk?.[0]).toBe(8)
  })

  it('updates the tab revision after its own configuration reset and permits the next activation', async () => {
    const session = useSessionStore()
    const key = session.vmk
    requests.mockResolvedValueOnce(new Response(null, { status: 204, headers: { 'X-Lock-Revision': '5' } }))
    await resetCredentials()
    expect(session.vmk).toBe(key)
    expect(session.serverLockRevision).toBe(5)
    await activateCredentials(input)
    expect(JSON.parse(String(requests.mock.calls[1]![1]?.body)).lock_revision).toBe(5)
  })

  it.each(['pending', 'completed'])('ignores an older background conflict while its own settings reset is %s', async phase => {
    let finishActivation!: (response: Response) => void
    let finishReset!: (response: Response) => void
    requests.mockImplementation((_, init) => new Promise(resolve => {
      if (init?.method === 'DELETE') finishReset = resolve
      else finishActivation = resolve
    }))
    const activation = activateCredentials(input)
    const reset = resetCredentials()
    const resetResponse = new Response(null, { status: 204, headers: { 'X-Lock-Revision': '5' } })
    if (phase === 'completed') { finishReset(resetResponse); await reset }
    finishActivation(Response.json({ error: { code: 'LOCK_REVISION_CONFLICT' } }, { status: 409 }))
    await expect(activation).rejects.toMatchObject({ code: 'LOCK_REVISION_CONFLICT' })
    if (phase === 'pending') { finishReset(resetResponse); await reset }
    expect(useSessionStore().unlocked).toBe(true)
    expect(useSessionStore().serverLockRevision).toBe(5)
  })

  it.each([null, 'invalid', '6'])('does not adopt an unverifiable or intervening remote lock revision (%s)', async revision => {
    const headers = new Headers()
    if (revision !== null) headers.set('X-Lock-Revision', revision)
    requests.mockResolvedValueOnce(new Response(null, { status: 204, headers }))
    await expect(resetCredentials()).rejects.toThrow('The session changed')
    expect(useSessionStore().unlocked).toBe(false)
    expect(useSessionStore().serverLockRevision).toBe(4)
  })

  it('discards a reset response if this tab locked while it was pending', async () => {
    requests.mockImplementationOnce(async () => {
      useSessionStore().lockLocal(false)
      return new Response(null, { status: 204, headers: { 'X-Lock-Revision': '5' } })
    })
    await expect(resetCredentials()).rejects.toThrow('The vault was locked')
    expect(useSessionStore().serverLockRevision).toBe(4)
  })
})
