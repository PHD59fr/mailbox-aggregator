import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App.vue'
import { createAppRouter } from './index'
import { api, APIError } from '../api/client'
import { listAccounts, listSMTPConfigs } from '../api/accounts'
import { sendMessage } from '../api/messages'
import RichTextEditor from '../components/RichTextEditor.vue'
import { useSessionStore } from '../stores/session'

const draftStore = vi.hoisted(() => ({ records: new Map<string, unknown>() }))

vi.mock('../api/client', async (importOriginal) => ({ ...await importOriginal<typeof import('../api/client')>(), api: vi.fn() }))
vi.mock('../api/accounts', async (importOriginal) => ({ ...await importOriginal<typeof import('../api/accounts')>(), listAccounts: vi.fn(), listSMTPConfigs: vi.fn() }))
vi.mock('../api/messages', () => ({ sendMessage: vi.fn() }))
vi.mock('../crypto/draftStorage', () => ({
  listStoredDrafts: async () => [...draftStore.records.values()],
  replaceStoredDraft: async (previous: unknown, record: unknown) => { const id=(record as {id:string}).id; if(JSON.stringify(draftStore.records.get(id))===JSON.stringify(previous)) draftStore.records.set(id,record) },
  getStoredDraft: async (id: string) => draftStore.records.get(id),
  putStoredDraft: async (record: unknown) => { draftStore.records.set((record as { id: string }).id, record) },
  deleteStoredDraft: async (id: string) => { draftStore.records.delete(id) },
  pruneStoredDrafts: async () => {},
}))

const view = { user_id: 'test-user', state: 'AUTHENTICATED_LOCKED' as const, csrf_token: 'test-csrf' }
let wrapper: VueWrapper | undefined

beforeEach(() => {
  vi.resetAllMocks()
  draftStore.records.clear()
  sessionStorage.clear()
  vi.stubGlobal('indexedDB', {})
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  vi.mocked(listAccounts).mockResolvedValue({})
  vi.mocked(listSMTPConfigs).mockResolvedValue({})
  vi.mocked(api).mockRejectedValue(new APIError('UNAUTHENTICATED', 'Authentication is required.', 401))
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.unstubAllGlobals() })

function createApplication() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory())
  return { pinia, router, session: useSessionStore() }
}

describe('initial navigation', () => {
  it('returns to the inbox after sending and shows a dismissible confirmation', async () => {
    const sender = { id: 'sender', name: 'Work', host: 'smtp.example.test', port: 465, security: 'tls' as const, username: 'sender', sender_email: 'sender@example.test', password: 'test-secret', is_default: true }
    vi.mocked(listSMTPConfigs).mockResolvedValue({ sender })
    vi.mocked(sendMessage).mockResolvedValue({ status: 'sent', message_id: '<sent@example.test>' })
    const { pinia, router, session } = createApplication()
    session.$patch({ loading: false, authenticated: true, userID: 'test-user', vmk: new Uint8Array(32).fill(7) })
    await router.push('/compose')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    await wrapper.find('#compose-to').setValue('to@example.test')
    await wrapper.find('#compose-subject').setValue('Hello')
    wrapper.findComponent(RichTextEditor).vm.$emit('update:modelValue', '<p>Hello <strong>team</strong></p>')
    wrapper.findComponent(RichTextEditor).vm.$emit('update:text', 'Hello team')
    await wrapper.find('form[aria-label="New message"]').trigger('submit')
    await flushPromises()
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/mail'))
    expect(wrapper.find('.compose-sheet').exists()).toBe(false)
    const notice=document.querySelector('.mail-notice[role="status"]')!
    expect(notice.textContent).toBe('Message sent.')
    expect(notice.parentElement).toBe(document.body)
    expect(wrapper.find('.mail-notice').exists()).toBe(false)
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ html: '<p>Hello <strong>team</strong></p>', text: 'Hello team' }), '')
    ;(notice.querySelector('button') as HTMLButtonElement).click();await flushPromises()
    expect(document.querySelector('.mail-notice[role="status"]')).toBeNull()
  })

  it('restores an encrypted compose draft after the workspace is replaced by the lock screen', async () => {
    const sender = { id: 'sender', name: 'Work', host: 'smtp.example.test', port: 465, security: 'tls' as const, username: 'sender', sender_email: 'sender@example.test', password: 'test-secret', is_default: true }
    vi.mocked(listSMTPConfigs).mockResolvedValue({ sender })
    vi.mocked(api).mockResolvedValue(undefined)
    const { pinia, router, session } = createApplication()
    session.$patch({ loading: false, authenticated: true, userID: 'test-user', csrf: 'test-csrf', vmk: new Uint8Array(32).fill(7) })
    await router.push('/compose')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    await wrapper.find('#compose-to').setValue('private@example.test')
    await wrapper.find('#compose-subject').setValue('Private subject')
    wrapper.findComponent(RichTextEditor).vm.$emit('update:modelValue', '<p>Private body</p>')
    wrapper.findComponent(RichTextEditor).vm.$emit('update:text', 'Private body')
    await flushPromises()
    const key = new Uint8Array(session.vmk!)

    await session.lock()
    await flushPromises()
    expect(session.unlocked).toBe(false)
    expect(wrapper.find('.vault-lock-screen').exists()).toBe(true)
    expect(wrapper.find('.compose-sheet').exists()).toBe(false)
    expect(wrapper.find('#compose-to').exists()).toBe(false)
    const saved = JSON.stringify([...draftStore.records.values()])
    expect(saved).not.toContain('private@example.test')
    expect(saved).not.toContain('Private subject')
    expect(saved).not.toContain('Private body')

    session.$patch({ vmk: key })
    await vi.waitFor(() => {
      expect((wrapper!.find('#compose-subject').element as HTMLInputElement).value).toBe('Private subject')
    })
    const recipient = wrapper.find('#compose-to').element as HTMLInputElement
    expect(recipient.value).toBe('private@example.test')
    expect(wrapper.findComponent(RichTextEditor).props('modelValue')).toBe('<p>Private body</p>')
    expect(wrapper.find('.compose-draft-restored').exists()).toBe(true)
  })

  it.each(['/mail', '/settings', '/compose'])('replaces the entire workspace at %s while locked', async path => {
    const { pinia, router, session } = createApplication()
    session.$patch({ loading: false, authenticated: true, userID: 'test-user' })
    await router.push(path)
    wrapper = mount(App, { attachTo: document.body, global: { plugins: [pinia, router] } })
    await flushPromises()
    expect(wrapper.find('.vault-lock-screen #unlock-title').text()).toBe('Unlock your mailbox')
    expect(wrapper.find('.app-shell').exists()).toBe(false)
    expect(wrapper.find('.app-sidebar').exists()).toBe(false)
    expect(wrapper.find('.app-header').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(document.activeElement).toBe(wrapper.find('input[type="password"]').element)
    expect(listSMTPConfigs).not.toHaveBeenCalled()
    expect(listAccounts).not.toHaveBeenCalled()
  })

  it('blocks the full workspace immediately when Lock vault is clicked, before the server responds', async () => {
    const { pinia, router, session } = createApplication()
    const key = new Uint8Array(32).fill(7)
    session.$patch({ loading: false, authenticated: true, userID: 'test-user', vmk: key })
    await router.push('/settings')
    wrapper = mount(App, { attachTo: document.body, global: { plugins: [pinia, router] } })
    await flushPromises()
    let finishLock!: () => void
    vi.mocked(api).mockImplementation(() => new Promise<void>(resolve => { finishLock = resolve }))
    await wrapper.find('#security button').trigger('click')
    expect(session.unlocked).toBe(false)
    expect(key.every(byte => byte === 0)).toBe(true)
    expect(wrapper.find('.vault-lock-screen').exists()).toBe(true)
    expect(wrapper.find('.app-sidebar').exists()).toBe(false)
    expect(wrapper.find('#security').exists()).toBe(false)
    expect(router.currentRoute.value.path).toBe('/settings')
    finishLock()
    await flushPromises()
    session.$patch({ vmk: new Uint8Array(32).fill(7) })
    await flushPromises()
    expect(wrapper.find('.vault-lock-screen').exists()).toBe(false)
    expect(wrapper.find('#security').exists()).toBe(true)
    expect(wrapper.find('.app-sidebar').exists()).toBe(true)
  })

  it('opens Add sender address when New message is clicked without SMTP configuration', async () => {
    const { pinia, router, session } = createApplication()
    session.$patch({ loading: false, authenticated: true, userID: 'test-user', vmk: new Uint8Array(32).fill(7) })
    await router.push('/mail')
    wrapper = mount(App, { attachTo: document.body, global: { plugins: [pinia, router] } })
    expect(wrapper.find('.header-compose').exists()).toBe(false)
    await wrapper.find('.app-sidebar [aria-label="New message"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/settings#identities')
    expect(wrapper.find('#identities h2').text()).toBe('Send email')
    expect(wrapper.find('#identities button').text()).toBe('Add sender address')
    expect(wrapper.find('[role="dialog"] h2').text()).toBe('Add sender address')
    await wrapper.find('[role="dialog"] [aria-label="Close"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.find('.app-sidebar [aria-label="New message"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="dialog"] h2').text()).toBe('Add sender address')
    expect(listSMTPConfigs).toHaveBeenCalled()
  })

  it.each(['/', '/mail', '/settings', '/compose'])('sends an anonymous visitor at %s to sign in', async path => {
    const { pinia, router } = createApplication()
    await router.push(path)
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await router.isReady()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(wrapper.find('h2').text()).toBe('Sign in')
    expect(wrapper.text()).not.toContain('Session ended')
    expect(wrapper.find('.app-header').exists()).toBe(false)
    expect(vi.mocked(api).mock.calls.filter(([path])=>path==='/session')).toHaveLength(1)
    expect(api).toHaveBeenCalledWith('/session')
  })

  it('opens the mailbox when a valid session is restored at the homepage', async () => {
    vi.mocked(api).mockResolvedValue(view)
    const { pinia, router, session } = createApplication()
    await router.push('/')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await router.isReady()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/mail')
    expect(session.authenticated).toBe(true)
    expect(wrapper.find('#unlock-title').text()).toBe('Unlock your mailbox')
    expect(wrapper.text()).not.toContain('Session ended')
    await router.push('/settings')
    expect(router.currentRoute.value.path).toBe('/settings')
    expect(vi.mocked(api).mock.calls.filter(([path])=>path==='/session')).toHaveLength(1)
  })

  it('shows loading while checking the session without displaying the mail screen', async () => {
    let finishRestore!: (value: typeof view) => void
    vi.mocked(api).mockImplementation(() => new Promise(resolve => { finishRestore = resolve }))
    const { pinia, router } = createApplication()
    const navigation = router.push('/')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await flushPromises()
    expect(wrapper.text()).toBe('Loading Mailbox Aggregator…')
    expect(wrapper.find('.app-header').exists()).toBe(false)
    finishRestore(view)
    await navigation
    await router.isReady()
    await flushPromises()
    expect(wrapper.find('#unlock-title').exists()).toBe(true)
    expect(vi.mocked(api).mock.calls.filter(([path])=>path==='/session')).toHaveLength(1)
  })

  it('keeps account registration accessible to anonymous visitors', async () => {
    const { pinia, router } = createApplication()
    await router.push('/register')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await router.isReady()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/register')
    expect(wrapper.find('h2').text()).toBe('Create your vault')
  })

  it('clears an invalid session and any previous vault key before showing sign in', async () => {
    const { pinia, router, session } = createApplication()
    const previousKey = new Uint8Array(32).fill(7)
    session.$patch({ authenticated: true, userID: 'previous-user', csrf: 'previous-csrf', vmk: previousKey })
    await router.push('/')
    wrapper = mount(App, { global: { plugins: [pinia, router] } })
    await router.isReady()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(session.userID).toBe('')
    expect(session.vmk).toBeNull()
    expect(previousKey.every(byte => byte === 0)).toBe(true)
  })
})
