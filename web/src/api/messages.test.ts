// @vitest-environment node
import { webcrypto } from 'node:crypto'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAccount, createSMTPConfig, linkSendingIdentity, listAccounts, listSMTPConfigs, deleteAccount, deleteSMTPConfig, updateAccount, updateSMTPConfig } from './accounts'
import { sendMessage, type MessageInput } from './messages'
import { encryptFile, decryptFile, generateVMK, type EncryptedFile } from '../crypto/vault'
import { useSessionStore } from '../stores/session'

const userID = 'f4e526bd-11df-4d31-a838-67087e5526cf'
const smtpInput = { name: 'Send only', host: 'smtp.example.test', port: 465, security: 'tls' as const, username: 'smtp-user', password: 'smtp-password', sender_name: 'Support', sender_email: 'support@example.test' }
let vmk: Uint8Array
let files: Record<string, EncryptedFile>
let requests: ReturnType<typeof vi.fn<typeof fetch>>
let outcomeUnknown: boolean
let receivingUnavailable:boolean
let lockRevision: number

beforeAll(() => { Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true }) })
beforeEach(async () => {
  setActivePinia(createPinia())
  vmk = generateVMK()
  const session = useSessionStore()
  session.accept({ user_id: userID, state: 'AUTHENTICATED_LOCKED', csrf_token: 'csrf' })
  session.vmk = vmk
  files = { accounts: await encryptFile([], vmk, userID, 'accounts'), smtp: await encryptFile([], vmk, userID, 'smtp') }
  outcomeUnknown = false
  receivingUnavailable=false
  lockRevision=0
  requests = vi.fn<typeof fetch>(async (url, init) => {
    const name = String(url).split('/').pop()!
    if (String(url).includes('/vault/files/')) {
      if (init?.method === 'PUT') {
        expect(new Headers(init.headers).get('If-Match')).toBe('"revision"')
        files[name] = JSON.parse(String(init.body)) as EncryptedFile
        return new Response(null, { status: 204 })
      }
      return Response.json(files[name], { headers: { ETag: '"revision"' } })
    }
    if (url === '/api/v1/session/credentials') {
      if (init?.method === 'DELETE') {
        lockRevision++
        return new Response(null, { status: 204, headers: { 'X-Lock-Revision': String(lockRevision) } })
      }
      expect(JSON.parse(String(init?.body)).lock_revision).toBe(lockRevision)
      if(receivingUnavailable && init?.method==='POST' && JSON.parse(String(init.body)).accounts)return Response.json({error:{code:'MAIL_SERVER_UNAVAILABLE',message:'IMAP temporarily unavailable.'}},{status:503})
      return Response.json({ state: 'AUTHENTICATED_UNLOCKED' })
    }
    if (url === '/api/v1/messages/send') {
      if (outcomeUnknown) return Response.json({ error: { code: 'SMTP_OUTCOME_UNKNOWN', message: 'Delivery could not be confirmed.' } }, { status: 502 })
      return Response.json({ status: 'sent', message_id: '<message@example.test>' })
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  vi.stubGlobal('fetch', requests)
})
afterEach(() => { vi.unstubAllGlobals() })

const draft = (id: string): MessageInput => ({ smtp_config_id: id, to: [{ email: 'to@example.test' }], cc: [], bcc: [], subject: 'Hello', text: 'SMTP only' })

describe('independent receiving and sending identities', () => {
  it('activates a linked IMAP mailbox and passes its Sent mapping without exposing its password in the draft',async()=>{
    const sender=await createSMTPConfig(smtpInput)
    const account=await createAccount({name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls',imap_username:'own',password:'imap-secret',smtp_config_id:sender.id,folder_mapping:{sent:'Work/Sent',archive:'Sent'},save_sent:true})
    await sendMessage({...draft(sender.id),sent_account_id:account.id})
    const submitted=requests.mock.calls.find(([url])=>url==='/api/v1/messages/send')!
    const payload=JSON.parse(String(submitted[1]?.body))
    expect(payload.sent_copy).toEqual({account_id:account.id,folder:'Work/Sent',exclude:['Sent']})
    expect(JSON.stringify(payload)).not.toContain('imap-secret');expect(payload).not.toHaveProperty('sent_account_id')
    const receiving=requests.mock.calls.find(([url,init])=>url==='/api/v1/session/credentials' && init?.method==='POST' && JSON.parse(String(init.body)).accounts)!
    expect(JSON.parse(String(receiving[1]?.body)).accounts[account.id].password).toBe('imap-secret')
    await updateAccount(account.id,{name:account.name,imap_host:account.imap_host,imap_port:993,imap_security:'tls',imap_username:'own',save_sent:false})
    expect((await listAccounts())[account.id]?.save_sent).toBe(false)
  })
  it('still sends once and returns a distinct copy warning if IMAP activation fails',async()=>{
    const sender=await createSMTPConfig(smtpInput)
    const account=await createAccount({name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls',imap_username:'own',password:'imap-secret',smtp_config_id:sender.id})
    receivingUnavailable=true
    const result=await sendMessage({...draft(sender.id),sent_account_id:account.id})
    expect(result.status).toBe('sent');expect(result.sent_copy?.status).toBe('failed')
    const sent=requests.mock.calls.filter(([url])=>url==='/api/v1/messages/send')
    expect(sent).toHaveLength(1);expect(JSON.parse(String(sent[0]![1]?.body))).not.toHaveProperty('sent_copy')
  })
  it('encrypts manual folder mappings, preserves omitted mappings and allows resetting to Automatic', async () => {
    const input = {name:'Receiving',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret'}
    const mapping = {trash:'Personal/Deleted été',sent:'Work/Sent'}
    const account = await createAccount({...input,folder_mapping:mapping})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual(mapping)
    expect(JSON.stringify(files.accounts)).not.toContain('Personal/Deleted')
    expect(JSON.stringify(files.accounts)).not.toContain('folder_mapping')
    await updateAccount(account.id,{...input,password:''})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual(mapping)
    await updateAccount(account.id,{...input,password:'',folder_mapping:{trash:'Other/Deleted'}})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual({trash:'Other/Deleted'})
    await updateAccount(account.id,{...input,password:'',folder_mapping:{trash:null,sent:null}})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual({trash:null,sent:null})
    await updateAccount(account.id,{...input,password:''})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual({trash:null,sent:null})
    await updateAccount(account.id,{...input,password:'',folder_mapping:{}})
    expect((await listAccounts())[account.id]?.folder_mapping).toEqual({})
  })
  it('encrypts, retains and toggles recipient-based reply selection independently of SMTP linking', async () => {
    const linked = await createSMTPConfig(smtpInput)
    const input = {name:'Receiving',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret'}
    const account = await createAccount({...input,smtp_config_id:linked.id,reply_from_recipient:true})
    expect((await listAccounts())[account.id]?.reply_from_recipient).toBe(true)
    expect(JSON.stringify(files.accounts)).not.toContain('reply_from_recipient')
    await updateAccount(account.id,{...input,password:''})
    expect((await listAccounts())[account.id]?.reply_from_recipient).toBe(true)
    await updateAccount(account.id,{...input,password:'',reply_from_recipient:false})
    expect((await listAccounts())[account.id]).toEqual({...account,reply_from_recipient:false})
  })
  it('encrypts and preserves the opt-in folder synchronization preference', async () => {
    const input = {name:'Receiving',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret',sync_folders:true}
    const account = await createAccount(input)
    expect((await listAccounts())[account.id]?.sync_folders).toBe(true)
    expect(JSON.stringify(files.accounts)).not.toContain('sync_folders')
    await updateAccount(account.id,{name:input.name,imap_host:input.imap_host,imap_port:input.imap_port,imap_security:input.imap_security,imap_username:input.imap_username,password:''})
    expect((await listAccounts())[account.id]?.sync_folders).toBe(true)
    await updateAccount(account.id,{...input,password:'',sync_folders:false})
    expect((await listAccounts())[account.id]?.sync_folders).toBe(false)
  })
  it('stores the IMAP password encrypted, preserves it on blank edits and supports replacement', async () => {
    const input = { name: 'Receiving', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'imap-secret' }
    const account = await createAccount(input)
    const firstWrite = requests.mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(String(firstWrite[1]?.body)).not.toContain('imap-secret')
    await updateAccount(account.id, { ...input, name: 'Updated', password: '' })
    expect((await listAccounts())[account.id]?.password).toBe('imap-secret')
    await updateAccount(account.id, { ...input, password: 'replacement-secret' })
    expect((await listAccounts())[account.id]?.password).toBe('replacement-secret')
  })
  it('updates a mailbox in place, retaining its default status and sender link', async () => {
    const sender = await createSMTPConfig(smtpInput)
    const input = { name: 'Receiver', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me' }
    const account = await createAccount({ ...input, smtp_config_id: sender.id })
    await updateAccount(account.id, { ...input, name: 'Updated', imap_host: 'new-imap.example.test', imap_port: 1993 })
    expect(await listAccounts()).toEqual({ [account.id]: { ...account, name: 'Updated', imap_host: 'new-imap.example.test', imap_port: 1993 } })
    expect((await listSMTPConfigs())[sender.id]).toEqual(sender)
  })

  it('preserves a saved SMTP password on a blank update without touching receiving identities', async () => {
    const sender = await createSMTPConfig(smtpInput)
    requests.mockClear()
    await updateSMTPConfig(sender.id, { ...smtpInput, name: 'Updated', host: 'new-smtp.example.test', password: '' })
    expect(await listSMTPConfigs()).toEqual({ [sender.id]: { ...sender, name: 'Updated', host: 'new-smtp.example.test' } })
    expect(requests.mock.calls.some(([url]) => url === '/api/v1/vault/files/accounts')).toBe(false)
    expect(requests.mock.calls.some(([url, init]) => url === '/api/v1/session/credentials' && init?.method === 'DELETE')).toBe(true)
  })

  it('replaces the SMTP password encrypted in place while preserving linked mailboxes', async () => {
    const sender = await createSMTPConfig(smtpInput)
    const account = await createAccount({ name: 'Receiver', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'receiver', smtp_config_id: sender.id })
    requests.mockClear()
    await updateSMTPConfig(sender.id, { ...smtpInput, sender_email: 'new@example.test', password: 'new-secret' })
    expect((await listSMTPConfigs())[sender.id]).toEqual({ ...sender, sender_email: 'new@example.test', password: 'new-secret' })
    expect((await listAccounts())[account.id]?.smtp_config_id).toBe(sender.id)
    const put = requests.mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(String(put[1]?.body)).not.toContain('new-secret')
    expect(String(put[1]?.body)).not.toContain('new@example.test')
  })

  it('reports edit conflicts without recreating an identity or overwriting stored values', async () => {
    const sender = await createSMTPConfig(smtpInput)
    const original = requests.getMockImplementation()!
    requests.mockImplementation(async (url, init) => url === '/api/v1/vault/files/smtp' && init?.method === 'PUT'
      ? Response.json({ error: { code: 'REVISION_CONFLICT', message: 'Concurrent edit.' } }, { status: 409 })
      : original(url, init))
    await expect(updateSMTPConfig(sender.id, { ...smtpInput, name: 'Updated' })).rejects.toThrow('Concurrent edit')
    expect(await listSMTPConfigs()).toEqual({ [sender.id]: sender })
  })

  it('does not recreate a deleted identity when an old edit form is submitted', async () => {
    await expect(updateSMTPConfig('missing', smtpInput)).rejects.toThrow('not found')
    expect(requests.mock.calls.some(([, init]) => init?.method === 'PUT' || init?.method === 'DELETE')).toBe(false)
  })

  it('does not save edited settings if the vault locks during credential revocation', async () => {
    const sender = await createSMTPConfig(smtpInput)
    const original = requests.getMockImplementation()!
    requests.mockClear()
    requests.mockImplementation(async (url, init) => {
      if (url === '/api/v1/session/credentials' && init?.method === 'DELETE') useSessionStore().lockLocal()
      return original(url, init)
    })
    await expect(updateSMTPConfig(sender.id, { ...smtpInput, name: 'Updated' })).rejects.toThrow('vault was locked')
    expect(requests.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('removes a mailbox without deleting its sender and promotes another mailbox as default', async () => {
    const config = await createSMTPConfig(smtpInput)
    const first = await createAccount({ name: 'First', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'first', smtp_config_id: config.id })
    const second = await createAccount({ name: 'Second', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'second' })
    requests.mockClear()
    await deleteAccount(first.id)
    expect(await listAccounts()).toEqual({ [second.id]: { ...second, is_default: true } })
    expect((await listSMTPConfigs())[config.id]).toBeDefined()
    expect(requests.mock.calls.some(([url, init]) => url === '/api/v1/session/credentials' && init?.method === 'DELETE')).toBe(true)
  })

  it('removes a sender, unlinks its mailboxes and keeps other senders', async () => {
    const first = await createSMTPConfig(smtpInput)
    const second = await createSMTPConfig({ ...smtpInput, name: 'Second sender' })
    const account = await createAccount({ name: 'Receiver', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'receiver', smtp_config_id: first.id })
    await deleteSMTPConfig(first.id)
    expect((await listAccounts())[account.id]).toEqual({ ...account, smtp_config_id: undefined })
    expect(await listSMTPConfigs()).toEqual({ [second.id]: { ...second, is_default: true } })
    const puts = requests.mock.calls.filter(([, init]) => init?.method === 'PUT')
    expect(puts.every(([, init]) => !String(init?.body).includes(smtpInput.password))).toBe(true)
  })

  it('does not delete saved configuration if active credentials cannot be revoked', async () => {
    const config = await createSMTPConfig(smtpInput)
    const original = requests.getMockImplementation()!
    requests.mockClear()
    requests.mockImplementation(async (url, init) => url === '/api/v1/session/credentials' && init?.method === 'DELETE'
      ? Response.json({ error: { code: 'UNAVAILABLE', message: 'Could not clear active credentials.' } }, { status: 503 })
      : original(url, init))
    await expect(deleteSMTPConfig(config.id)).rejects.toThrow('Could not clear active credentials')
    expect((await listSMTPConfigs())[config.id]).toBeDefined()
    expect(requests.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('keeps the sender if unlinking encounters a revision conflict', async () => {
    const config = await createSMTPConfig(smtpInput)
    const account = await createAccount({ name: 'Receiver', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'receiver', smtp_config_id: config.id })
    const original = requests.getMockImplementation()!
    requests.mockImplementation(async (url, init) => url === '/api/v1/vault/files/accounts' && init?.method === 'PUT'
      ? Response.json({ error: { code: 'REVISION_CONFLICT', message: 'Concurrent edit.' } }, { status: 409 })
      : original(url, init))
    await expect(deleteSMTPConfig(config.id)).rejects.toThrow('Concurrent edit')
    expect((await listSMTPConfigs())[config.id]).toBeDefined()
    expect((await listAccounts())[account.id]?.smtp_config_id).toBe(config.id)
  })

  it('reports partial unlinking without claiming that a sender was deleted on conflict', async () => {
    const config = await createSMTPConfig(smtpInput)
    const account = await createAccount({ name: 'Receiver', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'receiver', smtp_config_id: config.id })
    const original = requests.getMockImplementation()!
    requests.mockImplementation(async (url, init) => url === '/api/v1/vault/files/smtp' && init?.method === 'PUT'
      ? Response.json({ error: { code: 'REVISION_CONFLICT', message: 'Concurrent edit.' } }, { status: 409 })
      : original(url, init))
    await expect(deleteSMTPConfig(config.id)).rejects.toThrow('mailboxes were kept and unlinked')
    expect((await listSMTPConfigs())[config.id]).toBeDefined()
    expect((await listAccounts())[account.id]?.smtp_config_id).toBeUndefined()
  })

  it('does not save a removal if the vault locks during credential revocation', async () => {
    const config = await createSMTPConfig(smtpInput)
    const original = requests.getMockImplementation()!
    requests.mockClear()
    requests.mockImplementation(async (url, init) => {
      if (url === '/api/v1/session/credentials' && init?.method === 'DELETE') useSessionStore().lockLocal()
      return original(url, init)
    })
    await expect(deleteSMTPConfig(config.id)).rejects.toThrow('vault was locked')
    expect(requests.mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
  })

  it('saves a sending identity and its password encrypted without accessing IMAP accounts', async () => {
    const config = await createSMTPConfig(smtpInput)
    expect(await listSMTPConfigs()).toEqual({ [config.id]: config })
    expect(requests.mock.calls.every(([url]) => url === '/api/v1/vault/files/smtp')).toBe(true)
    const put = requests.mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(String(put[1]?.body)).not.toContain(smtpInput.password)
    await expect(decryptFile(files.smtp!, vmk, userID, 'smtp')).resolves.toEqual({ [config.id]: config })
  })

  it('sends with SMTP alone and keeps credentials out of the message payload', async () => {
    const config = await createSMTPConfig(smtpInput)
    requests.mockClear()
    await expect(sendMessage(draft(config.id))).resolves.toMatchObject({ status: 'sent' })
    expect(requests.mock.calls.map(([url]) => url)).toEqual(['/api/v1/vault/files/smtp', '/api/v1/session/credentials', '/api/v1/messages/send'])
    const activation = JSON.parse(String(requests.mock.calls[1]![1]?.body))
    expect(activation.accounts).toBeUndefined()
    expect(activation.smtp[config.id]).toMatchObject({ password: smtpInput.password, sender_email: smtpInput.sender_email })
    const submission = String(requests.mock.calls[2]![1]?.body)
    expect(submission).not.toContain(smtpInput.password)
    expect(JSON.parse(submission)).not.toHaveProperty('from')
  })

  it('links and unlinks receiving and sending identities without deleting either', async () => {
    const config = await createSMTPConfig(smtpInput)
    const account = await createAccount({ name: 'Receive only', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'receiver' })
    expect(account.smtp_config_id).toBeUndefined()
    await linkSendingIdentity(account.id, config.id)
    expect((await listAccounts())[account.id]?.smtp_config_id).toBe(config.id)
    await linkSendingIdentity(account.id, '')
    expect((await listAccounts())[account.id]?.smtp_config_id).toBeUndefined()
    expect((await listSMTPConfigs())[config.id]).toBeDefined()
  })

  it('keeps an ambiguous submission visible and never automatically retries', async () => {
    const config = await createSMTPConfig(smtpInput)
    outcomeUnknown = true
    await expect(sendMessage(draft(config.id))).rejects.toMatchObject({ code: 'SMTP_OUTCOME_UNKNOWN' })
    expect(requests.mock.calls.filter(([url]) => url === '/api/v1/messages/send')).toHaveLength(1)
  })

  it('does not send if the vault is locked while activating the SMTP credentials', async () => {
    const config = await createSMTPConfig(smtpInput)
    const original = requests.getMockImplementation()!
    requests.mockImplementation(async (url, init) => {
      if (url === '/api/v1/session/credentials') useSessionStore().lockLocal()
      return original(url, init)
    })
    await expect(sendMessage(draft(config.id))).rejects.toThrow('Unlock your vault')
    expect(requests.mock.calls.filter(([url]) => url === '/api/v1/messages/send')).toHaveLength(0)
  })
})
