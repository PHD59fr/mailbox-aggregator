import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import FolderManager from './FolderManager.vue'
import { manageFolder } from '../api/reading'
import { useSessionStore } from '../stores/session'

vi.mock('../api/reading', () => ({ manageFolder: vi.fn() }))
const props = { accountID: 'work', accountName: 'Work', action: 'rename' as const, folder: 'Projects/2026' }
beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  useSessionStore().$patch({ authenticated: true, userID: 'user', vmk: new Uint8Array(32) })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = true }) })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = false }) })
  vi.mocked(manageFolder).mockResolvedValue(undefined)
})
afterEach(() => { document.body.innerHTML = '' })
const options = { global: { stubs: { teleport: true } } }

describe('contextual folder actions', () => {
  it('renames the selected full folder path without selecting another folder', async () => {
    const wrapper = mount(FolderManager, { props, ...options })
    expect(wrapper.get('input').element.value).toBe('Projects/2026')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('input').setValue('Projects/2027')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(manageFolder).toHaveBeenCalledWith('work', 'rename', 'Projects/2026', 'Projects/2027', '', expect.any(AbortSignal))
    expect(wrapper.emitted('changed')).toEqual([['rename', 'Projects/2026', 'Projects/2027']])
    wrapper.unmount()
  })

  it('requires an exact folder name before destructive deletion', async () => {
    const wrapper = mount(FolderManager, { props: { ...props, action: 'delete' }, ...options })
    expect(wrapper.text()).toContain('permanently delete its messages')
    await wrapper.get('input').setValue('2026')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(manageFolder).not.toHaveBeenCalled()
    await wrapper.get('input').setValue('Projects/2026')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(manageFolder).toHaveBeenCalledWith('work', 'delete', 'Projects/2026', '', 'Projects/2026', expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('creates a folder from the plus action and keeps provider errors visible', async () => {
    vi.mocked(manageFolder).mockRejectedValue(new Error('Folder already exists.'))
    const wrapper = mount(FolderManager, { props: { ...props, action: 'create', folder: '' }, ...options })
    await wrapper.get('input').setValue('Receipts')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(manageFolder).toHaveBeenCalledWith('work', 'create', 'Receipts', '', '', expect.any(AbortSignal))
    expect(wrapper.get('[role="alert"]').text()).toBe('Folder already exists.')
    expect(wrapper.emitted('changed')).toBeUndefined()
    wrapper.unmount()
  })

  it('cancels pending work on vault lock and ignores a late successful response', async () => {
    let finish!: () => void
    vi.mocked(manageFolder).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mount(FolderManager, { props: { ...props, action: 'subscribe' }, ...options })
    await wrapper.get('form').trigger('submit')
    const signal = vi.mocked(manageFolder).mock.calls[0]![5]
    useSessionStore().lockLocal(); await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.emitted('close')).toBeTruthy()
    finish(); await flushPromises()
    expect(wrapper.emitted('changed')).toBeUndefined()
    wrapper.unmount()
  })
})
