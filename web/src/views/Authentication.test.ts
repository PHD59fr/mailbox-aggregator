import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from './LoginView.vue'
import RegisterView from './RegisterView.vue'
import { loginWithOpaque, registerWithOpaque } from '../crypto/auth'
import { unlockVault, type VaultMetadata } from '../crypto/vault'
import { api,APIError } from '../api/client'
import { useSessionStore } from '../stores/session'
import { HTTPS_REQUIRED_MESSAGE } from '../crypto/secureContext'

const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('../crypto/auth', () => ({ loginWithOpaque: vi.fn(), registerWithOpaque: vi.fn() }))
vi.mock('../crypto/vault', async (importOriginal) => ({ ...await importOriginal<typeof import('../crypto/vault')>(), unlockVault: vi.fn() }))
vi.mock('../api/client', async (importOriginal) => ({ ...await importOriginal<typeof import('../api/client')>(), api: vi.fn() }))
vi.mock('../crypto/tabResume',async original=>({...await original<typeof import('../crypto/tabResume')>(),saveTabResume:vi.fn().mockResolvedValue(false)}))

const view = { user_id: 'test-user', state: 'AUTHENTICATED_LOCKED' as const, csrf_token: 'test-csrf' }
const password = 'correct horse battery staple'
const metadata = { version: 1 } as VaultMetadata

afterEach(() => vi.unstubAllGlobals())

beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
  vi.mocked(loginWithOpaque).mockResolvedValue(view)
  vi.mocked(registerWithOpaque).mockResolvedValue(view)
  vi.mocked(api).mockImplementation(async path => path === '/auth/options' ? {registration_enabled:true} : path === '/session' ? view : metadata)
  vi.mocked(unlockVault).mockResolvedValue(new Uint8Array(32).fill(7))
})

function mountLogin() {
  return mount(LoginView, { global: { stubs: { RouterLink: true } } })
}

async function fillLogin(wrapper: ReturnType<typeof mountLogin>) {
  await wrapper.find('input[autocomplete="username"]').setValue('Me@example.test')
  await wrapper.find('input[type="password"]').setValue(password)
}

describe('signing in unlocks the browser vault', () => {
  it.each([LoginView, RegisterView])('explains HTTP access before entering credentials or making requests', async component => {
    vi.stubGlobal('isSecureContext', false)
    const wrapper = mount(component, { global: { stubs: { RouterLink: true } } })
    expect(wrapper.get('[role="alert"]').text()).toBe(HTTPS_REQUIRED_MESSAGE)
    expect(wrapper.get('button.primary').attributes()).toHaveProperty('disabled')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(loginWithOpaque).not.toHaveBeenCalled(); expect(registerWithOpaque).not.toHaveBeenCalled()
    expect(api).not.toHaveBeenCalled(); expect(push).not.toHaveBeenCalled()
  })
  it('explains a missing session cookie after successful authentication without blaming the password',async()=>{
    vi.mocked(api).mockRejectedValue(new APIError('UNAUTHENTICATED','Authentication is required.',401))
    const wrapper=mountLogin();await fillLogin(wrapper);await wrapper.find('form').trigger('submit');await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('HTTPS')
    expect(wrapper.get('[role="alert"]').text()).toContain('Sign-in succeeded')
    expect(wrapper.get('[role="alert"]').text()).toContain('not an incorrect password')
    expect(unlockVault).not.toHaveBeenCalled();expect(useSessionStore().authenticated).toBe(false);expect(push).not.toHaveBeenCalled()
  })
  it('summarizes three privacy benefits without adding extra login fields', () => {
    const wrapper = mountLogin()
    const benefits = wrapper.find('.auth-benefits')
    expect(benefits.findAll('li')).toHaveLength(3)
    expect(benefits.text()).toContain('Encrypted settings')
    expect(benefits.text()).toContain('Private sign-in')
    expect(benefits.text()).toContain('No email archive')
    expect(wrapper.findAll('input')).toHaveLength(2)
  })

  it('unlocks with the entered password before opening the mailbox', async () => {
    const wrapper = mountLogin()
    await fillLogin(wrapper)
    const session = useSessionStore()
    push.mockImplementation(() => { expect(session.unlocked).toBe(true) })
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(loginWithOpaque).toHaveBeenCalledWith('Me@example.test', password)
    expect(api).toHaveBeenCalledWith('/vault/metadata')
    expect(unlockVault).toHaveBeenCalledWith(password, view.user_id, metadata)
    expect(session.login).toBe('me@example.test')
    expect(push).toHaveBeenCalledWith('/mail')
    expect((wrapper.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('waits for decryption and reuses the password that authenticated even if the input changes', async () => {
    let finishLogin!: (value: typeof view) => void
    vi.mocked(loginWithOpaque).mockImplementation(() => new Promise(resolve => { finishLogin = resolve }))
    let finishUnlock!: (key: Uint8Array) => void
    vi.mocked(unlockVault).mockImplementation(() => new Promise(resolve => { finishUnlock = resolve }))
    const wrapper = mountLogin()
    await fillLogin(wrapper)
    await wrapper.find('form').trigger('submit')
    await wrapper.find('input[type="password"]').setValue('changed while waiting')
    finishLogin(view)
    await flushPromises()
    expect(unlockVault).toHaveBeenCalledWith(password, view.user_id, metadata)
    expect(push).not.toHaveBeenCalled()
    expect(wrapper.find('button').attributes()).toHaveProperty('disabled')
    finishUnlock(new Uint8Array(32).fill(7))
    await flushPromises()
    expect(push).toHaveBeenCalledWith('/mail')
  })

  it('does not attempt to unlock after failed authentication', async () => {
    vi.mocked(loginWithOpaque).mockRejectedValue(new Error('Authentication failed.'))
    const wrapper = mountLogin()
    await fillLogin(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(unlockVault).not.toHaveBeenCalled()
    expect(vi.mocked(api).mock.calls.filter(([path])=>path!=='/auth/options')).toHaveLength(0)
    expect(push).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toBe('Authentication failed.')
  })

  it('shows a vault failure without navigating or retaining a previous key', async () => {
    const session = useSessionStore()
    const previousKey = new Uint8Array(32).fill(9)
    session.$patch({ authenticated: true, userID: 'previous-user', csrf: 'previous-csrf', vmk: previousKey })
    vi.mocked(unlockVault).mockRejectedValue(new Error('Vault decryption failed.'))
    const wrapper = mountLogin()
    await fillLogin(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(push).not.toHaveBeenCalled()
    expect(session.unlocked).toBe(false)
    expect(previousKey.every(byte => byte === 0)).toBe(true)
    expect(wrapper.find('[role="alert"]').text()).toBe('Vault decryption failed.')
    expect((wrapper.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
  })

  it('also unlocks after registration with the password already entered', async () => {
    const wrapper = mount(RegisterView, { global: { stubs: { RouterLink: true } } })
    await wrapper.find('input[autocomplete="username"]').setValue('Me@example.test')
    for (const input of wrapper.findAll('input[type="password"]')) await input.setValue(password)
    await wrapper.find('input[type="checkbox"]').setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(registerWithOpaque).toHaveBeenCalledWith('Me@example.test', password)
    expect(unlockVault).toHaveBeenCalledWith(password, view.user_id, metadata)
    expect(useSessionStore().unlocked).toBe(true)
    expect(push).toHaveBeenCalledWith('/mail')
    for (const input of wrapper.findAll('input[type="password"]')) expect((input.element as HTMLInputElement).value).toBe('')
  })
})

describe('vault lifecycle', () => {
  it('keeps the vault locked locally even if server credential removal fails', async () => {
    const session = useSessionStore()
    const key = new Uint8Array(32).fill(7)
    session.$patch({ authenticated: true, userID: view.user_id, vmk: key })
    vi.mocked(api).mockRejectedValue(new Error('Network unavailable'))
    await expect(session.lock()).rejects.toThrow('Network unavailable')
    expect(session.unlocked).toBe(false)
    expect(key.every(byte => byte === 0)).toBe(true)
  })

  it('restores the HTTP session after a page reload with the vault still locked', async () => {
    vi.mocked(api).mockResolvedValue(view)
    const session = useSessionStore()
    await session.restore()
    expect(session.authenticated).toBe(true)
    expect(session.unlocked).toBe(false)
    expect(unlockVault).not.toHaveBeenCalled()
  })

  it.each(['lock', 'logout'] as const)('discards a pending decryption after %s', async action => {
    const session = useSessionStore()
    session.accept(view)
    const key = new Uint8Array(32).fill(7)
    let finishUnlock!: (key: Uint8Array) => void
    vi.mocked(unlockVault).mockImplementation(() => new Promise(resolve => { finishUnlock = resolve }))
    const unlocking = session.unlock(password)
    const rejected = expect(unlocking).rejects.toThrow('The session changed.')
    await flushPromises()
    await session[action]()
    finishUnlock(key)
    await rejected
    expect(session.unlocked).toBe(false)
    expect(key.every(byte => byte === 0)).toBe(true)
  })
})

it('does not register on an instance which has closed registrations',async()=>{
 vi.mocked(api).mockImplementation(async path=>path==='/auth/options'?{registration_enabled:false}:view)
 const wrapper=mount(RegisterView,{global:{stubs:{RouterLink:true}}});await flushPromises()
 expect(wrapper.text()).toContain('Registration is closed')
 await wrapper.find('form').trigger('submit');expect(registerWithOpaque).not.toHaveBeenCalled()
})
