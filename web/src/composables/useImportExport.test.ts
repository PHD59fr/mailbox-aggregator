import { beforeEach, afterEach, it, expect, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSessionStore } from '../stores/session'
import { encryptExportBundle, decryptExportBundle, exportUserData, importUserData, type ExportBundle } from './useImportExport'
import { encryptFile, decryptFile, type EncryptedFile } from '../crypto/vault'

vi.mock('../crypto/argon2', async original => ({...await original<typeof import('../crypto/argon2')>(),derivePasswordKey: async () => new Uint8Array(32).fill(9)}))
const account = { id: 'work', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'work', is_default: true, password: 'imap-secret', smtp_config_id: 'sender' }
const sender = { id: 'sender', name: 'Sender', host: 'smtp.example.test', port: 465, security: 'tls', username: 'work', is_default: true, password: 'smtp-secret', sender_email: 'work@example.test' }
const bundle = (): ExportBundle => ({ version: 1, exported_at: new Date().toISOString(), accounts: { work: account }, smtp: { sender }, preferences: {} })
let envelopes: Record<string, EncryptedFile>
let requests: ReturnType<typeof vi.fn<typeof fetch>>
beforeEach(async () => {
  setActivePinia(createPinia())
  const session = useSessionStore()
  session.accept({ user_id: 'user', csrf_token: 'csrf', state: 'AUTHENTICATED_LOCKED', lock_revision: 0 })
  session.$patch({ vmk: new Uint8Array(32).fill(7), resumeOnReload: false })
  envelopes = Object.fromEntries(await Promise.all(['accounts', 'smtp', 'preferences'].map(async name => [name, await encryptFile({}, session.vmk!, 'user', name)])))
  requests = vi.fn<typeof fetch>(async (url, init) => {
    if (url === '/api/v1/session/credentials' && init?.method === 'DELETE') return new Response(null, { status: 204, headers: { 'X-Lock-Revision': '1' } })
    if (url === '/api/v1/vault/import') return new Response(null, { status: 204 })
    const name = String(url).split('/').pop()!
    return Response.json(envelopes[name], { headers: { 'X-Resource-Revision': `"${name}-revision"` } })
  })
  vi.stubGlobal('fetch', requests)
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it('round trips a 200 kB encrypted export without a spread argument overflow', async () => {
  const data = bundle(); data.preferences = { note: { id: 'note', text: 'a'.repeat(200000) } }
  const encoded = await encryptExportBundle(data, 'password123')
  expect(encoded).not.toContain('imap-secret')
  expect(await decryptExportBundle(encoded, 'password123')).toEqual(data)
})
it('rejects incomplete exports instead of converting failed requests into empty collections', async () => {
  requests.mockRejectedValue(new TypeError('offline'))
  await expect(exportUserData('password123')).rejects.toThrow('offline')
})
it('exports every fetched collection and cancels an export when the owner changes', async () => {
  envelopes.accounts = await encryptFile({ work: account }, useSessionStore().vmk!, 'user', 'accounts')
  envelopes.smtp = await encryptFile({ sender }, useSessionStore().vmk!, 'user', 'smtp')
  const result = await decryptExportBundle(await exportUserData('password123'), 'password123')
  expect(result.accounts).toEqual({ work: account })
  requests.mockImplementation(async () => { useSessionStore().lockLocal(false); return Response.json(envelopes.accounts, { headers: { ETag: 'revision' } }) })
  await expect(exportUserData('password123')).rejects.toThrow('changed')
})
it('validates all records and submits one encrypted revision guarded import after clearing credentials', async () => {
  const vmk = useSessionStore().vmk!.slice()
  await importUserData(await encryptExportBundle(bundle(), 'password123'), 'password123')
  const index = requests.mock.calls.findIndex(([url]) => url === '/api/v1/vault/import')
  expect(String(requests.mock.calls[index - 1]![0])).toBe('/api/v1/session/credentials')
  const request = requests.mock.calls[index]![1]!
  expect(String(request.body)).not.toContain('imap-secret')
  const files = JSON.parse(String(request.body)).files
  expect(Object.keys(files)).toEqual(['accounts', 'smtp', 'preferences'])
  for (const name of Object.keys(files)) expect(files[name].revision).toBe(`"${name}-revision"`)
  expect(await decryptFile(files.accounts.data, vmk, 'user', 'accounts')).toEqual({ work: account })
  expect(useSessionStore().authenticated).toBe(false)
})
it('rejects invalid links or configured limits before any network mutation', async () => {
  const data = bundle(); data.accounts.work = { ...account, smtp_config_id: 'missing' }
  await expect(importUserData(await encryptExportBundle(data, 'password123'), 'password123')).rejects.toThrow('missing sender')
  expect(requests).not.toHaveBeenCalled()
  useSessionStore().maxAccounts = 1
  data.accounts = { work: account, second: { ...account, id: 'second' } }
  await expect(importUserData(await encryptExportBundle(data, 'password123'), 'password123')).rejects.toThrow('at most 1')
  expect(requests).not.toHaveBeenCalled()
})
it('keeps conflicts explicit and never automatically repeats an ambiguous import', async () => {
  const original = requests.getMockImplementation()!
  requests.mockImplementation(async (url, init) => url === '/api/v1/vault/import'
    ? Response.json({ error: { code: 'REVISION_CONFLICT', message: 'changed remotely' } }, { status: 409 }) : original(url, init))
  const data = await encryptExportBundle(bundle(), 'password123')
  await expect(importUserData(data, 'password123')).rejects.toMatchObject({ code: 'REVISION_CONFLICT' })
  expect(requests.mock.calls.filter(([url]) => url === '/api/v1/vault/import')).toHaveLength(1)
  requests.mockImplementation(async (url, init) => url === '/api/v1/vault/import' ? Promise.reject(new TypeError('connection lost')) : original(url, init))
  // A second explicit user attempt uses its own lock revision.
  useSessionStore().serverLockRevision = 0
  await expect(importUserData(data, 'password123')).rejects.toMatchObject({ code: 'IMPORT_UNCONFIRMED' })
  expect(requests.mock.calls.filter(([url]) => url === '/api/v1/vault/import')).toHaveLength(2)
})
it('rejects malformed envelope lengths and unsupported KDFs before deriving a key', async () => {
  const valid = JSON.parse(await encryptExportBundle(bundle(), 'password123'))
  await expect(decryptExportBundle(JSON.stringify({ ...valid, nonce: 'AAAA' }), 'password123')).rejects.toThrow('Invalid export envelope')
  await expect(decryptExportBundle(JSON.stringify({ ...valid, kdf_params: { ...valid.kdf_params, memory_kib: 1 } }), 'password123')).rejects.toThrow('Unsupported')
})
