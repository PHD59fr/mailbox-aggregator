import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ConnectionTest from './ConnectionTest.vue'
import { api } from '../api/client'
import { useSessionStore } from '../stores/session'
vi.mock('../api/client', () => ({ api: vi.fn() }))
beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); setActivePinia(createPinia()); useSessionStore().$patch({ authenticated: true, vmk: new Uint8Array(32) }) })
afterEach(() => vi.useRealTimers())
const props = { protocol: 'imap' as const, host: 'imap.example.test', port: 993, security: 'tls', username: 'me', password: 'private-probe' }
describe('connection probe', () => {
  it('never exposes the password in DOM, sends once and shares a 30-second cooldown', async () => {
    vi.mocked(api).mockResolvedValue({ status: 'connected' })
    const wrapper = mount(ConnectionTest,{props}); const smtp = mount(ConnectionTest,{ props: { ...props, protocol: 'smtp' } })
    expect(wrapper.html()).not.toContain(props.password)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(api).toHaveBeenCalledTimes(1); expect(api).toHaveBeenCalledWith('/connections/test',expect.objectContaining({ method: 'POST', body: expect.stringContaining('private-probe') }))
    expect(wrapper.text()).toContain('succeeded'); expect(smtp.get('button').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(30000); expect(smtp.get('button').attributes('disabled')).toBeUndefined()
    wrapper.unmount(); smtp.unmount()
  })
  it('cancels an obsolete result when credentials change', async () => {
    let done!: (value: unknown) => void; vi.mocked(api).mockImplementation(() => new Promise(resolve => { done = resolve }))
    const wrapper = mount(ConnectionTest,{props}); await wrapper.get('button').trigger('click')
    await wrapper.setProps({ password: 'replacement' }); done({ status: 'connected' }); await flushPromises()
    expect(wrapper.text()).not.toContain('succeeded'); wrapper.unmount()
  })
})
