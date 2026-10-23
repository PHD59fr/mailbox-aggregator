import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ContactPicker from './ContactPicker.vue'
import { listContactBook } from '../api/preferences'
import { useSessionStore } from '../stores/session'

vi.mock('../api/preferences', () => ({ listContactBook: vi.fn() }))
const book = { contacts: [
  { id: 'a', name: 'Alex', email: 'alex@example.test', group_ids: ['work'] },
  { id: 'b', name: 'Beth', email: 'beth@example.test', group_ids: ['work'] },
  { id: 'c', name: 'Chris', email: 'chris@example.test', group_ids: ['friends'] },
], groups: [{ id: 'work', name: 'Work' }, { id: 'friends', name: 'Friends' }] }
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  useSessionStore().$patch({ authenticated: true, userID: 'user', vmk: new Uint8Array(32) })
  vi.mocked(listContactBook).mockResolvedValue(book)
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = true }) })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = false }) })
})
const open = (existing: string[] = [], limit = 100) => mount(ContactPicker, { props: { target: 'To', existing, limit }, global: { stubs: { teleport: true, RouterLink: true } } })

describe('recipient contact picker', () => {
  it('selects contacts across groups and excludes addresses already added', async () => {
    const wrapper = open(['ALEX@example.test'], 2); await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[aria-label="Select Alex"]').element.checked).toBe(true)
    expect(wrapper.get('[aria-label="Select Alex"]').attributes('disabled')).toBeDefined()
    await wrapper.get('select').setValue('work')
    await wrapper.get('.contact-picker-tools button').trigger('click')
    await wrapper.get('select').setValue('friends')
    await wrapper.get('.contact-picker-tools button').trigger('click')
    await wrapper.get('footer .primary').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[book.contacts.slice(1)]])
    wrapper.unmount()
  })
  it('enforces the shared recipient limit and allows removing a selection', async () => {
    const wrapper = open([], 1); await flushPromises()
    await wrapper.get('[aria-label="Select Alex"]').setValue(true)
    expect(wrapper.get('[aria-label="Select Beth"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="Select Alex"]').setValue(false)
    expect(wrapper.get('[aria-label="Select Beth"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('input[type="search"]').setValue('beth@')
    expect(wrapper.findAll('.contact-picker-list li')).toHaveLength(1)
    await wrapper.get('.contact-picker-tools button').trigger('click')
    await wrapper.get('footer .primary').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[[book.contacts[1]]]])
    wrapper.unmount()
  })
  it('does not add contacts when the vault has locked', async () => {
    const wrapper = open(); await flushPromises()
    await wrapper.get('[aria-label="Select Alex"]').setValue(true)
    useSessionStore().lockLocal(); await flushPromises()
    await wrapper.get('footer .primary').trigger('click')
    expect(wrapper.emitted('select')).toBeUndefined()
    wrapper.unmount()
  })
})
