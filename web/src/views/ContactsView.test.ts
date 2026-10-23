import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import ContactsView from './ContactsView.vue'
import { deleteContactGroup, listContactBook, saveContact, saveContactGroup } from '../api/preferences'
import { useSessionStore } from '../stores/session'

vi.mock('../api/preferences', () => ({ listContactBook: vi.fn(), saveContact: vi.fn(), deleteContact: vi.fn(), saveContactGroup: vi.fn(), deleteContactGroup: vi.fn() }))
const book = {
  contacts: [{ id: 'a', name: 'Alex', email: 'alex@example.test', group_ids: ['work'] }, { id: 'm', name: 'Mary', email: 'mary@example.test', group_ids: ['friends'] }, { id: 'z', name: 'Zoe', email: 'zoe@example.test' }],
  groups: [{ id: 'work', name: 'Work' }, { id: 'friends', name: 'Friends' }],
}
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  useSessionStore().$patch({ loading: false, authenticated: true, userID: 'user', vmk: new Uint8Array(32) })
  vi.mocked(listContactBook).mockResolvedValue(structuredClone(book))
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = true }) })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: vi.fn(function (this: HTMLDialogElement) { this.open = false }) })
})
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/contacts', component: { template: '<div />' } }] })
  await router.push('/contacts')
  const wrapper = mount(ContactsView, { global: { plugins: [router], stubs: { AppHeader: true, teleport: true } } })
  await flushPromises(); return wrapper
}
describe('contacts workspace', () => {
  it('filters contacts by group, ungrouped and search', async () => {
    const wrapper = await open()
    expect(wrapper.findAll('.contact-list > li')).toHaveLength(3)
    await wrapper.findAll('.contact-group-choice').find(button => button.text().includes('Work'))!.trigger('click')
    expect(wrapper.get('.contact-list').text()).toContain('Alex')
    expect(wrapper.get('.contact-list').text()).not.toContain('Mary')
    await wrapper.findAll('.contact-group-choice').find(button => button.text().includes('Ungrouped'))!.trigger('click')
    expect(wrapper.get('.contact-list').text()).toContain('Zoe')
    await wrapper.findAll('.contact-group-choice')[0]!.trigger('click')
    await wrapper.get('input[type="search"]').setValue('mary@')
    expect(wrapper.findAll('.contact-list > li')).toHaveLength(1)
    expect(wrapper.get('.contact-list').text()).toContain('Mary')
    wrapper.unmount()
  })
  it('saves contact membership and keeps duplication explicit', async () => {
    vi.mocked(saveContact).mockResolvedValue(book.contacts)
    const wrapper = await open()
    await wrapper.get('[aria-label="Edit contact Alex"]').trigger('click')
    const checks = wrapper.findAll<HTMLInputElement>('.contacts-dialog input[type="checkbox"]')
    await checks.find(input => input.element.value === 'friends')!.setValue(true)
    await wrapper.get('.contacts-dialog form').trigger('submit'); await flushPromises()
    expect(saveContact).toHaveBeenCalledWith({ id: 'a', name: 'Alex', email: 'alex@example.test', group_ids: ['work', 'friends'] })
    await wrapper.get('[aria-label="Duplicate contact Alex"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('.contacts-dialog input[type="email"]').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('.contacts-dialog input[maxlength="128"]').element.value).toBe('Alex (copy)')
    wrapper.unmount()
  })
  it('creates a group and confirms deleting it while retaining contacts', async () => {
    vi.mocked(saveContactGroup).mockResolvedValue({ ...book, groups: [...book.groups, { id: 'family', name: 'Family' }] })
    vi.mocked(deleteContactGroup).mockResolvedValue({ contacts: book.contacts.map(contact => ({ ...contact, group_ids: [] })), groups: [book.groups[1]!] })
    const wrapper = await open()
    await wrapper.get('[aria-label="New group"]').trigger('click')
    await wrapper.get('.contacts-dialog input').setValue('Family')
    await wrapper.get('.contacts-dialog form').trigger('submit'); await flushPromises()
    expect(saveContactGroup).toHaveBeenCalledWith({ id: '', name: 'Family' })
    await wrapper.get('[aria-label="Edit group Work"]').trigger('click')
    await wrapper.get('.contact-delete-group').trigger('click')
    expect(deleteContactGroup).not.toHaveBeenCalled()
    expect(wrapper.get('.contacts-dialog').text()).toContain('contacts will stay')
    await wrapper.get('.contacts-dialog form').trigger('submit'); await flushPromises()
    expect(deleteContactGroup).toHaveBeenCalledWith('work')
    expect(wrapper.findAll('.contact-list > li')).toHaveLength(3)
    wrapper.unmount()
  })
  it('ignores a contact load that completes after locking', async () => {
    let finish!: (value: typeof book) => void
    vi.mocked(listContactBook).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = await open()
    useSessionStore().lockLocal(); await flushPromises()
    finish(book); await flushPromises()
    expect(wrapper.find('.contacts-page').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('alex@example.test')
    wrapper.unmount()
  })
})
