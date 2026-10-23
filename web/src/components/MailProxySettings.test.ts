import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MailProxySettings from './MailProxySettings.vue'
import { apiResponse } from '../api/client'
import { readCollection } from '../api/vaultCollection'
import { useSessionStore } from '../stores/session'

vi.mock('../api/client', async importOriginal => ({ ...await importOriginal<typeof import('../api/client')>(), apiResponse: vi.fn() }))
vi.mock('../api/vaultCollection', () => ({ readCollection: vi.fn() }))

const account = { id: 'inbox', name: 'Work inbox', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'provider-user', password: 'imap-secret', is_default: true }
const sender = { id: 'sender', name: 'Work sender', host: 'smtp.example.test', port: 587, security: 'starttls' as const, username: 'smtp-user', sender_email: 'work@example.test', password: 'smtp-secret', is_default: true }
const available = { imap_available: true, smtp_available: true, hostname: 'mail.example.test', imap_port: 993, smtp_port: 465, imap_account_id: '', smtp_config_id: '', needs_update: false }
const response = (value = available, revision = '*') => new Response(JSON.stringify(value), { headers: { ETag: revision } })
const open = () => mount(MailProxySettings, { props: { accounts: [account], senders: [sender] } })

beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
  useSessionStore().$patch({ authenticated: true, userID: 'alice', login: 'alice@example.test', vmk: new Uint8Array(32) })
  vi.mocked(apiResponse).mockImplementation(async () => response())
})

describe('mail client access settings', () => {
  it('shows separate protocol hostnames and the configured public web URL', async () => {
    vi.mocked(apiResponse).mockResolvedValue(new Response(JSON.stringify({ ...available, imap_hostname: 'imap.public.test', smtp_hostname: 'smtp.public.test', web_url: 'https://webmail.public.test' })))
    const wrapper = open()
    await flushPromises()
    expect(wrapper.text()).toContain('imap.public.test')
    expect(wrapper.text()).toContain('smtp.public.test')
    expect(wrapper.text()).not.toContain('mail.example.test')
    expect(wrapper.get('a[href="https://webmail.public.test"]').text()).toBe('https://webmail.public.test')
    wrapper.unmount()
  })

  it('uses the shared hostname when per-protocol names are absent', async () => {
    const wrapper = open()
    await flushPromises()
    expect(wrapper.text().match(/mail\.example\.test/g)).toHaveLength(2)
    wrapper.unmount()
  })

  it('stays hidden when the server has no enabled proxy', async () => {
    vi.mocked(apiResponse).mockResolvedValue(response({ ...available, imap_available: false, smtp_available: false }))
    const wrapper = open()
    await flushPromises()
    expect(wrapper.find('section').exists()).toBe(false)
    wrapper.unmount()
  })

  it('saves the selected providers with their revisions and a dedicated password', async () => {
    const wrapper = open()
    await flushPromises()
    const checks = wrapper.findAll('input[type="checkbox"]')
    await checks[0]!.setValue(true)
    await checks[1]!.setValue(true)
    await wrapper.findAll('select')[0]!.setValue(account.id)
    await wrapper.findAll('select')[1]!.setValue(sender.id)
    await wrapper.get('#proxy-password').setValue('dedicated-client-password-123')
    vi.mocked(readCollection).mockResolvedValueOnce({ records: { [account.id]: account }, revision: 'accounts-revision', save: vi.fn(), assertUnlocked: vi.fn() })
      .mockResolvedValueOnce({ records: { [sender.id]: sender }, revision: 'smtp-revision', save: vi.fn(), assertUnlocked: vi.fn() })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const put = vi.mocked(apiResponse).mock.calls.find(([, init]) => init?.method === 'PUT')!
    expect(put[0]).toBe('/proxy')
    expect(put[1]?.headers).toEqual({ 'If-Match': '*' })
    expect(JSON.parse(put[1]?.body as string)).toMatchObject({ password: 'dedicated-client-password-123', accounts_revision: 'accounts-revision', smtp_revision: 'smtp-revision', imap: { id: account.id, password: 'imap-secret' }, smtp: { id: sender.id, password: 'smtp-secret' } })
    expect(useSessionStore().notice).toContain('Mail client access saved')
    expect(wrapper.text()).toContain('alice@example.test')
    expect(wrapper.text()).not.toContain('imap-secret')
    wrapper.unmount()
  })

  it('generates a strong password and reports stale provider settings', async () => {
    vi.mocked(apiResponse).mockImplementation(async () => response({ ...available, imap_account_id: account.id, needs_update: true }, 'proxy-revision'))
    const wrapper = open()
    await flushPromises()
    expect(wrapper.text()).toContain('Your provider settings changed')
    await wrapper.findAll('button').find(button => button.text() === 'Generate password')!.trigger('click')
    const input = wrapper.get<HTMLInputElement>('#proxy-password')
    expect(input.element.value).toMatch(/^[0-9a-f]{48}$/)
    expect(input.attributes('type')).toBe('text')
    wrapper.unmount()
  })

  it('copies the generated proxy password and resets feedback for a new password', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const wrapper = open(); await flushPromises()
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(true)
    const copy = wrapper.get('[aria-label="Copy proxy password"]')
    expect(copy.attributes('disabled')).toBeDefined()
    await wrapper.findAll('button').find(button => button.text() === 'Generate password')!.trigger('click')
    const password = wrapper.get<HTMLInputElement>('#proxy-password').element.value
    await copy.trigger('click'); await flushPromises()
    expect(writeText).toHaveBeenCalledWith(password)
    expect(wrapper.text()).toContain('Password copied.')
    expect(wrapper.text()).not.toContain(password)
    await wrapper.get('#proxy-password').setValue('a-new-proxy-password')
    expect(wrapper.text()).not.toContain('Password copied.')
    wrapper.unmount()
  })

  it('offers a manual copy fallback when clipboard access is denied', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error('Permission denied')) } })
    const wrapper = open(); await flushPromises()
    await wrapper.findAll('input[type="checkbox"]')[0]!.setValue(true)
    await wrapper.get('#proxy-password').setValue('a-manually-entered-proxy-password')
    await wrapper.get('[aria-label="Copy proxy password"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('select and copy it manually')
    expect(wrapper.text()).not.toContain('Permission denied')
    wrapper.unmount()
  })

  it('can enable all sending addresses from the current encrypted profile', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.findAll('input[type="checkbox"]')[1]!.setValue(true)
    expect(wrapper.get('select').element.value).toBe('*')
    await wrapper.get('#proxy-password').setValue('dedicated-client-password-123')
    const alias = { ...sender, id: 'alias', sender_email: 'alias@example.test' }
    vi.mocked(readCollection).mockResolvedValue({ records: { sender, alias }, revision: 'smtp-revision', save: vi.fn(), assertUnlocked: vi.fn() })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const put = vi.mocked(apiResponse).mock.calls.find(([, init]) => init?.method === 'PUT')!
    const body = JSON.parse(put[1]?.body as string)
    expect(body.senders.map((route: { sender_email: string }) => route.sender_email)).toEqual(['work@example.test', 'alias@example.test'])
    expect(body.smtp_revision).toBe('smtp-revision')
    expect(body.smtp).toBeUndefined()
    wrapper.unmount()
  })

  it('can revoke old access even after the server proxy is disabled', async () => {
    vi.mocked(apiResponse).mockImplementation(async () => response({ ...available, imap_available: false, smtp_available: false, imap_account_id: account.id }, 'proxy-revision'))
    const wrapper = open()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Disable client access')!.trigger('click')
    await flushPromises()
    expect(apiResponse).toHaveBeenCalledWith('/proxy', { method: 'DELETE', headers: { 'If-Match': 'proxy-revision' } })
    expect(readCollection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('uses the resource revision when a reverse proxy changes the ETag', async () => {
    vi.mocked(apiResponse).mockImplementation(async () => new Response(JSON.stringify({ ...available, imap_account_id: account.id }), {
      headers: { ETag: 'W/"transport"', 'X-Resource-Revision': '"stored-proxy-revision"' },
    }))
    const wrapper = open()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'Disable client access')!.trigger('click')
    await flushPromises()
    expect(apiResponse).toHaveBeenCalledWith('/proxy', { method: 'DELETE', headers: { 'If-Match': '"stored-proxy-revision"' } })
    wrapper.unmount()
  })

  it('does not save credentials if the vault locks while reading identities', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('select').setValue(account.id)
    await wrapper.get('#proxy-password').setValue('dedicated-client-password-123')
    let resolve!: (value: Awaited<ReturnType<typeof readCollection>>) => void
    vi.mocked(readCollection).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await wrapper.get('form').trigger('submit')
    useSessionStore().lockLocal(false)
    resolve({ records: { [account.id]: account }, revision: 'accounts-revision', save: vi.fn(), assertUnlocked: vi.fn() })
    await flushPromises()
    expect(vi.mocked(apiResponse).mock.calls.some(([, init]) => init?.method === 'PUT')).toBe(false)
    wrapper.unmount()
  })
})
