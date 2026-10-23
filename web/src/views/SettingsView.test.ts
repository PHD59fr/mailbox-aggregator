import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SettingsView from './SettingsView.vue'
import SMTPModal from '../components/SMTPModal.vue'
import AccountModal from '../components/AccountModal.vue'
import { listAccounts, listSMTPConfigs, linkSendingIdentity, deleteAccount, deleteSMTPConfig } from '../api/accounts'
import { useSessionStore } from '../stores/session'

vi.mock('../api/accounts', () => ({ listAccounts: vi.fn(), listSMTPConfigs: vi.fn(), linkSendingIdentity: vi.fn(), deleteAccount: vi.fn(), deleteSMTPConfig: vi.fn() }))

const receiver = { id: 'receiver', name: 'Receive only', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'receiver@example.test', is_default: true }
const sender = { id: 'sender', name: 'Send only', host: 'smtp.example.test', port: 465, security: 'tls' as const, username: 'smtp-user', sender_email: 'sender@example.test', is_default: true }

beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
  useSessionStore().$patch({ authenticated: true, userID: 'test-user', vmk: new Uint8Array(32).fill(7) })
  vi.mocked(listAccounts).mockResolvedValue({ [receiver.id]: { ...receiver } })
  vi.mocked(listSMTPConfigs).mockResolvedValue({ [sender.id]: sender })
  vi.mocked(linkSendingIdentity).mockResolvedValue(undefined)
  vi.mocked(deleteAccount).mockResolvedValue(undefined)
  vi.mocked(deleteSMTPConfig).mockResolvedValue(undefined)
})

async function openSettings(path = '/') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/settings', component: { template: '<div />' } }, { path: '/compose', component: { template: '<div />' } }] })
  await router.push(path)
  const wrapper = mount(SettingsView, { global: { plugins: [router], stubs: { AppHeader: true, VaultUnlock: true, AccountModal: true, SMTPModal: true } } })
  await flushPromises()
  return wrapper
}

describe('optional identity links', () => {
  it('opens each existing identity for editing, and clears the target when adding', async () => {
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Edit mailbox Receive only"]').trigger('click')
    const mailbox = wrapper.findComponent(AccountModal)
    expect(mailbox.props('modelValue')).toBe(true)
    expect(mailbox.props('account')).toEqual(receiver)
    mailbox.vm.$emit('update:modelValue', false)
    await flushPromises()
    await wrapper.findAll('#accounts > button')[0]!.trigger('click')
    expect(mailbox.props('account')).toBeNull()
    await wrapper.find('[aria-label="Edit sender Send only"]').trigger('click')
    const smtp = wrapper.findComponent(SMTPModal)
    expect(smtp.props('modelValue')).toBe(true)
    expect(smtp.props('config')).toEqual(sender)
    expect(mailbox.props('modelValue')).toBe(false)
    smtp.vm.$emit('update:modelValue', false)
    await flushPromises()
    await wrapper.findAll('#identities > button')[0]!.trigger('click')
    expect(smtp.props('config')).toBeNull()
  })

  it('refreshes the identity after saving an edit', async () => {
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Edit sender Send only"]').trigger('click')
    vi.mocked(listSMTPConfigs).mockResolvedValue({ [sender.id]: { ...sender, name: 'Updated sender' } })
    wrapper.findComponent(SMTPModal).vm.$emit('success')
    await flushPromises()
    expect(wrapper.find('#identities li').text()).toContain('Updated sender')
    expect(wrapper.findComponent(SMTPModal).props('modelValue')).toBe(false)
  })

  it('uses clear section titles without the old extra headings', async () => {
    const wrapper = await openSettings()
    expect(wrapper.find('#accounts .eyebrow').exists()).toBe(false)
    expect(wrapper.find('#identities .eyebrow').exists()).toBe(false)
    expect(wrapper.find('#security .eyebrow').exists()).toBe(false)
    expect(wrapper.findAll('.settings-section-icon')).toHaveLength(4)
  })

  it('requires confirmation and supports cancelling before removing a mailbox', async () => {
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Remove mailbox Receive only"]').trigger('click')
    expect(deleteAccount).not.toHaveBeenCalled()
    expect(wrapper.find('#accounts .identity-removal').text()).toContain('emails stay with your provider')
    await wrapper.find('#accounts .identity-removal .secondary').trigger('click')
    expect(wrapper.find('.identity-removal').exists()).toBe(false)
    expect(deleteAccount).not.toHaveBeenCalled()
    await wrapper.find('[aria-label="Remove mailbox Receive only"]').trigger('click')
    await wrapper.find('#accounts .identity-removal .danger-button').trigger('click')
    await flushPromises()
    expect(deleteAccount).toHaveBeenCalledExactlyOnceWith(receiver.id)
    expect(wrapper.find('#accounts li').exists()).toBe(false)
    expect(wrapper.find('#identities').text()).toContain('Send only')
    expect(deleteSMTPConfig).not.toHaveBeenCalled()
  })

  it('removes a sender while retaining its linked receiving mailbox', async () => {
    vi.mocked(listAccounts).mockResolvedValue({ [receiver.id]: { ...receiver, smtp_config_id: sender.id } })
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Remove sender Send only"]').trigger('click')
    expect(deleteSMTPConfig).not.toHaveBeenCalled()
    await wrapper.find('#identities .identity-removal .danger-button').trigger('click')
    await flushPromises()
    expect(deleteSMTPConfig).toHaveBeenCalledExactlyOnceWith(sender.id)
    expect(wrapper.find('#identities li').exists()).toBe(false)
    expect(wrapper.find('#accounts li').text()).toContain('Receive only')
    expect((wrapper.find('#accounts select').element as HTMLSelectElement).value).toBe('')
    expect(deleteAccount).not.toHaveBeenCalled()
  })

  it('keeps the identity visible and reports a failed removal', async () => {
    vi.mocked(deleteSMTPConfig).mockRejectedValue(new Error('The file changed since it was read.'))
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Remove sender Send only"]').trigger('click')
    await wrapper.find('#identities .identity-removal .danger-button').trigger('click')
    await flushPromises()
    expect(wrapper.find('#identities li').text()).toContain('Send only')
    expect(wrapper.find('.identity-removal [role="alert"]').text()).toContain('file changed')
    expect(deleteSMTPConfig).toHaveBeenCalledTimes(1)
  })

  it('blocks duplicate removals and concurrent linking while a removal is pending', async () => {
    let finish!: () => void
    vi.mocked(deleteAccount).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await openSettings()
    await wrapper.find('[aria-label="Remove mailbox Receive only"]').trigger('click')
    await wrapper.find('.identity-removal .danger-button').trigger('click')
    expect(wrapper.find('#accounts select').attributes()).toHaveProperty('disabled')
    expect(wrapper.find('[aria-label="Remove sender Send only"]').attributes()).toHaveProperty('disabled')
    await wrapper.find('.identity-removal .danger-button').trigger('click')
    expect(deleteAccount).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
  })

  it('does not open the sender form on an ordinary settings visit', async () => {
    const wrapper = await openSettings('/settings#identities')
    expect(wrapper.findComponent(SMTPModal).props('modelValue')).toBe(false)
  })

  it('opens the sender form once when requested and waits for vault unlock', async () => {
    const session = useSessionStore()
    session.lockLocal()
    const wrapper = await openSettings('/settings?add=sender#identities')
    const modal = wrapper.findComponent(SMTPModal)
    expect(modal.props('modelValue')).toBe(false)
    session.$patch({ vmk: new Uint8Array(32).fill(7) })
    await flushPromises()
    expect(modal.props('modelValue')).toBe(true)
    modal.vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(modal.props('modelValue')).toBe(false)
    session.lockLocal()
    await flushPromises()
    session.$patch({ vmk: new Uint8Array(32).fill(7) })
    await flushPromises()
    expect(modal.props('modelValue')).toBe(false)
    wrapper.unmount()
  })

  it('shows a sending identity and allows adding another with no receiving identity', async () => {
    vi.mocked(listAccounts).mockResolvedValue({})
    const wrapper = await openSettings()
    expect(wrapper.find('#identities').text()).toContain('Send only')
    expect(wrapper.find('#accounts h2').text()).toBe('Receive email')
    expect(wrapper.find('#identities h2').text()).toBe('Send email')
    const add = wrapper.findAll('button').find(button => button.text() === 'Add sender address')!
    expect(add.attributes('disabled')).toBeUndefined()
    await add.trigger('click')
    expect(wrapper.findComponent(SMTPModal).props('modelValue')).toBe(true)
  })

  it('links and unlinks identities and offers composing with the selected sender', async () => {
    const wrapper = await openSettings()
    const select = wrapper.find('#accounts select')
    expect((select.element as HTMLSelectElement).value).toBe('')
    await select.setValue(sender.id)
    await flushPromises()
    expect(linkSendingIdentity).toHaveBeenLastCalledWith(receiver.id, sender.id)
    expect(wrapper.find('#accounts a').attributes('href')).toBe('/compose?from=sender')
    await select.setValue('')
    await flushPromises()
    expect(linkSendingIdentity).toHaveBeenLastCalledWith(receiver.id, '')
    expect(wrapper.find('#accounts a').exists()).toBe(false)
    expect(wrapper.find('#identities').text()).toContain('Send only')
  })

  it('restores the previous link when saving fails', async () => {
    vi.mocked(linkSendingIdentity).mockRejectedValue(new Error('The file changed since it was read.'))
    const wrapper = await openSettings()
    await wrapper.find('#accounts select').setValue(sender.id)
    await flushPromises()
    expect((wrapper.find('#accounts select').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.find('#accounts [role="alert"]').text()).toContain('file changed')
  })
})
