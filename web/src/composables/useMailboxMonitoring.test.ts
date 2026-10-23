import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useMailboxMonitoring } from './useMailboxMonitoring'
import { listAccounts } from '../api/accounts'
import { watchMailboxes } from '../api/mailboxes'
import { useSessionStore } from '../stores/session'
import { useMailboxStore } from '../stores/mailboxes'
import { APIError } from '../api/client'

vi.mock('../api/accounts', () => ({ listAccounts: vi.fn() }))
vi.mock('../api/mailboxes', () => ({ watchMailboxes: vi.fn() }))
const account = { id: 'receiver', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'imap-secret', is_default: true }
const status = { account_id: 'receiver', messages: 1, unseen: 1, uid_next: 2, uid_validity: 7, mode: 'idle' as const }
const Monitor = defineComponent({ setup() { useMailboxMonitoring(); return () => null } })
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  useSessionStore().$patch({ authenticated: true, userID: 'test-user', vmk: new Uint8Array(32).fill(7) })
  vi.mocked(listAccounts).mockResolvedValue({ receiver: account })
  Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
  vi.mocked(watchMailboxes).mockImplementation(async (_interval, signal, receive) => {
    receive(status)
    await new Promise<void>(resolve => signal.addEventListener('abort', () => resolve(), { once: true }))
  })
})
afterEach(() => { vi.useRealTimers(); localStorage.removeItem('mailbox-refresh-seconds') })

describe('visible browser monitoring', () => {
  it('recovers automatically when loading identities fails temporarily', async () => {
    vi.useFakeTimers()
    vi.mocked(listAccounts).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    const wrapper = mount(Monitor); await flushPromises()
    expect(watchMailboxes).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(3000)
    expect(watchMailboxes).toHaveBeenCalledTimes(1); expect(useMailboxStore().error).toBe('')
    wrapper.unmount()
  })
  it('backs off DNS reconnection, resumes immediately online and cancels on lock', async () => {
    vi.useFakeTimers()
    vi.mocked(watchMailboxes).mockRejectedValue(new APIError('MAIL_SERVER_UNAVAILABLE','DNS unavailable',503))
    const wrapper = mount(Monitor); await flushPromises()
    await vi.advanceTimersByTimeAsync(3000); expect(watchMailboxes).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5999); expect(watchMailboxes).toHaveBeenCalledTimes(2)
    globalThis.dispatchEvent(new Event('online')); await flushPromises(); expect(watchMailboxes).toHaveBeenCalledTimes(3)
    useSessionStore().lockLocal(); await flushPromises(); await vi.advanceTimersByTimeAsync(60000)
    expect(watchMailboxes).toHaveBeenCalledTimes(3); wrapper.unmount()
  })
  it('pauses when hidden, reconnects when visible and clears data when locked', async () => {
    const wrapper = mount(Monitor); await flushPromises()
    expect(useMailboxStore().unseen).toBe(1)
    const signal = vi.mocked(watchMailboxes).mock.calls[0]![1]
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(useMailboxStore().monitoring).toBe(false)
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(watchMailboxes).toHaveBeenCalledTimes(2)
    useSessionStore().lockLocal(); await flushPromises()
    expect(useMailboxStore().accounts).toEqual([])
    expect(useMailboxStore().statuses).toEqual({})
    wrapper.unmount()
  })

  it('announces a new UID but not the initial mailbox status', async () => {
    const wrapper = mount(Monitor); await flushPromises()
    expect(useSessionStore().notice).toBe('')
    const receive = vi.mocked(watchMailboxes).mock.calls[0]![2]
    receive({ ...status, uid_next: 3, unseen: 2 })
    expect(useSessionStore().notice).toBe('New mail in Work.')
    wrapper.unmount()
    expect(useMailboxStore().accounts).toEqual([])
  })

  it('applies the configured fallback interval and supports manual refresh', async () => {
    const wrapper = mount(Monitor); await flushPromises()
    const first = vi.mocked(watchMailboxes).mock.calls[0]![1]
    useMailboxStore().setInterval(60); await flushPromises()
    expect(first.aborted).toBe(true)
    expect(watchMailboxes).toHaveBeenLastCalledWith(60, expect.any(AbortSignal), expect.any(Function))
    useMailboxStore().refresh(); await flushPromises()
    expect(watchMailboxes).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })
})
