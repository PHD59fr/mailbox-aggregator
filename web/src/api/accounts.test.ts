// @vitest-environment node
import { webcrypto } from 'node:crypto'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAccount, listAccounts } from './accounts'
import { decryptFile, encryptFile, generateVMK, type EncryptedFile } from '../crypto/vault'
import { useSessionStore } from '../stores/session'

const userID = 'f4e526bd-11df-4d31-a838-67087e5526cf'
const input = { name: 'Personal', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me@example.test' }
let vmk: Uint8Array
let stored: EncryptedFile
let revision: string
let requests: ReturnType<typeof vi.fn<typeof fetch>>
let conflict: boolean

beforeAll(() => { Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true }) })
beforeEach(async () => {
  setActivePinia(createPinia())
  vmk = generateVMK()
  useSessionStore().$patch({ authenticated: true, userID, vmk })
  stored = await encryptFile([], vmk, userID, 'accounts')
  revision = '"first"'
  conflict = false
  requests = vi.fn<typeof fetch>(async (url, init) => {
    expect(url).toBe('/api/v1/vault/files/accounts')
    if (init?.method === 'PUT') {
      expect(new Headers(init.headers).get('If-Match')).toBe(revision)
      if (conflict) return Response.json({ error: { code: 'REVISION_CONFLICT', message: 'The file changed since it was read.' } }, { status: 409 })
      stored = JSON.parse(String(init.body)) as EncryptedFile
      revision = '"second"'
      return new Response(null, { status: 204, headers: { ETag: revision } })
    }
    return Response.json(stored, { headers: { ETag: revision } })
  })
  vi.stubGlobal('fetch', requests)
})
afterEach(() => { vi.unstubAllGlobals() })

describe('encrypted IMAP account persistence', () => {
  it('creates an IMAP account without SMTP from a newly registered vault and reads it back', async () => {
    const account = await createAccount(input)
    expect(account).toMatchObject({ ...input, is_default: true })
    expect(account.smtp_config_id).toBeUndefined()
    expect(await listAccounts()).toEqual({ [account.id]: account })
    const put = requests.mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(String(put[1]?.body)).not.toContain(input.imap_host)
    expect(String(put[1]?.body)).not.toContain(input.imap_username)
    await expect(decryptFile(stored, vmk, userID, 'accounts')).resolves.toEqual({ [account.id]: account })
  })

  it('preserves existing accounts and assigns the default only to the first account', async () => {
    const first = await createAccount(input)
    const second = await createAccount({ ...input, name: 'Work' })
    expect(second.is_default).toBe(false)
    expect(await listAccounts()).toEqual({ [first.id]: first, [second.id]: second })
  })

  it('reads an existing array collection', async () => {
    const existing = { ...input, id: 'existing', is_default: true }
    stored = await encryptFile([existing], vmk, userID, 'accounts')
    const account = await createAccount({ ...input, name: 'Work' })
    expect(await listAccounts()).toEqual({ existing, [account.id]: account })
  })

  it.each(['weakened', 'removed'])('saves using the resource revision when a proxy has %s the ETag', async (transformation) => {
    const headers = new Headers({ 'X-Resource-Revision': revision })
    if (transformation === 'weakened') headers.set('ETag', `W/${revision}`)
    requests.mockImplementationOnce(async () => Response.json(stored, { headers }))
    const account = await createAccount(input)
    expect(await listAccounts()).toEqual({ [account.id]: account })
  })

  it('reports concurrent edits without overwriting the stored collection', async () => {
    const original = stored
    requests.mockImplementationOnce(async () => Response.json(stored, { headers: {
      'X-Resource-Revision': revision, ETag: `W/${revision}`,
    } }))
    conflict = true
    await expect(createAccount(input)).rejects.toMatchObject({ code: 'REVISION_CONFLICT', status: 409 })
    expect(stored).toBe(original)
  })

  it('requires an unlocked vault before reading or writing', async () => {
    useSessionStore().lockLocal()
    await expect(createAccount(input)).rejects.toThrow('Unlock your vault first.')
    expect(requests).not.toHaveBeenCalled()
  })

  it('does not write when the vault is locked during the read', async () => {
    requests.mockImplementationOnce(async () => {
      useSessionStore().lockLocal()
      return Response.json(stored, { headers: { ETag: revision } })
    })
    await expect(createAccount(input)).rejects.toThrow('The vault was locked.')
    expect(requests).toHaveBeenCalledTimes(1)
  })

  it('does not overwrite a collection that cannot be decrypted', async () => {
    stored = await encryptFile([], generateVMK(), userID, 'accounts')
    await expect(createAccount(input)).rejects.toThrow()
    expect(requests).toHaveBeenCalledTimes(1)
  })
})

it('honors the account limit supplied by this instance', async()=>{
 const session=useSessionStore();session.maxAccounts=1
 await createAccount(input)
 const before=requests.mock.calls.length
 await expect(createAccount({...input,name:'Second'})).rejects.toThrow('Maximum number of mailboxes (1)')
 expect(requests.mock.calls.slice(before).every(([,init])=>init?.method!=='PUT')).toBe(true)
 session.maxAccounts=3
 await expect(createAccount({...input,name:'Second'})).resolves.toMatchObject({name:'Second'})
})
