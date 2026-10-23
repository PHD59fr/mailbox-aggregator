import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AccountModal from './AccountModal.vue'
import { createAccount, updateAccount } from '../api/accounts'
import { listFolders } from '../api/reading'

vi.mock('../api/accounts', () => ({ createAccount: vi.fn(), updateAccount: vi.fn() }))
vi.mock('../api/reading', () => ({ listFolders: vi.fn() }))
beforeEach(() => { vi.resetAllMocks(); setActivePinia(createPinia()) })

async function fillAccount(wrapper: ReturnType<typeof mount>) {
  const inputs = wrapper.findAll('input')
  await inputs[0]!.setValue('Personal')
  await inputs[1]!.setValue('imap.example.test')
  await inputs[3]!.setValue('me@example.test')
  await wrapper.find('input[type="password"]').setValue('imap-secret')
}

describe('IMAP account creation', () => {
  it('edits the recipient-based reply preference and resets it for a new account', async () => {
    const account = {id:'existing',name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret',is_default:true,reply_from_recipient:true}
    const wrapper = mount(AccountModal,{props:{modelValue:true,account}})
    const option = () => wrapper.findAll('label').find(label => label.text().includes('Prefer replying from'))!.find<HTMLInputElement>('input')
    expect(option().element.checked).toBe(true)
    expect(wrapper.text()).toContain('first available sender')
    await option().setValue(false); await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(updateAccount).toHaveBeenCalledWith('existing',expect.objectContaining({reply_from_recipient:false,password:''}))
    await wrapper.setProps({modelValue:false,account:null});await wrapper.setProps({modelValue:true})
    expect(option().element.checked).toBe(false)
    wrapper.unmount()
  })
  it('stores the opt-in folder synchronization preference on an existing mailbox', async () => {
    const account = {id:'existing',name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret',is_default:true,sync_folders:true}
    const wrapper = mount(AccountModal,{props:{modelValue:true,account}})
    const option = wrapper.findAll('label').find(label => label.text().includes('Synchronize IMAP folders'))!.find<HTMLInputElement>('input')
    expect(option.element.checked).toBe(true)
    await option.setValue(false); await wrapper.find('form').trigger('submit'); await flushPromises()
    expect(updateAccount).toHaveBeenCalledWith('existing',expect.objectContaining({sync_folders:false,password:''}))
    wrapper.unmount()
  })
  it('never prefills the saved IMAP password into the edit form', () => {
    const account = { id: 'saved', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'private-imap-password', is_default: true }
    const wrapper = mount(AccountModal, { props: { modelValue: true, account } })
    const password = wrapper.find<HTMLInputElement>('input[type="password"]')
    expect(password.element.value).toBe('')
    expect(password.attributes('required')).toBeUndefined()
    expect(wrapper.html()).not.toContain(account.password)
    expect(password.attributes('placeholder')).toContain('keep current password')
  })
  it('edits a prefilled mailbox in place and preserves a custom saved port', async () => {
    vi.mocked(updateAccount).mockResolvedValue(undefined)
    const account = { id: 'existing', name: 'Work', imap_host: 'imap.example.test', imap_port: 1143, imap_security: 'starttls' as const, imap_username: 'me', smtp_config_id: 'sender', is_default: true }
    const wrapper = mount(AccountModal, { props: { modelValue: true, account } })
    expect(wrapper.find('h2').text()).toBe('Edit mailbox')
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('1143')
    expect(wrapper.find('select').element.value).toBe('starttls')
    await wrapper.find('input').setValue('Updated work')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(updateAccount).toHaveBeenCalledExactlyOnceWith('existing', { name: 'Updated work', imap_host: account.imap_host, imap_port: 1143, imap_security: 'starttls', imap_username: 'me', password: '',mark_as_read:false,sync_folders:false,reply_from_recipient:false,save_sent:true,folder_mapping:{} })
    expect(createAccount).not.toHaveBeenCalled()
    expect(account.name).toBe('Work')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('cancels editing without mutating the identity and resets when adding a new one', async () => {
    const account = { id: 'existing', name: 'Work', imap_host: 'imap.example.test', imap_port: 11993, imap_security: 'tls' as const, imap_username: 'me', is_default: true }
    const wrapper = mount(AccountModal, { props: { modelValue: true, account } })
    await wrapper.find('input').setValue('Unsaved')
    await wrapper.find('.modal-actions .secondary').trigger('click')
    expect(updateAccount).not.toHaveBeenCalled()
    expect(account.name).toBe('Work')
    await wrapper.setProps({ modelValue: false, account: null })
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.find('h2').text()).toBe('Add mailbox')
    expect(wrapper.find('input').element.value).toBe('')
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('993')
    await wrapper.setProps({ account })
    expect(wrapper.find<HTMLInputElement>('input[type="number"]').element.value).toBe('11993')
  })

  it('preserves edited values and keeps the dialog open after a conflict', async () => {
    vi.mocked(updateAccount).mockRejectedValue(new Error('Concurrent edit.'))
    const account = { id: 'existing', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', is_default: true }
    const wrapper = mount(AccountModal, { props: { modelValue: true, account } })
    await wrapper.find('input').setValue('Updated')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('Concurrent edit')
    expect(wrapper.find('input').element.value).toBe('Updated')
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('opens directly on IMAP and saves without asking for SMTP', async () => {
    vi.mocked(createAccount).mockResolvedValue({ id: 'account', name: 'Personal', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'me@example.test', is_default: true })
    const wrapper = mount(AccountModal, { props: { modelValue: false } })
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.find('h2').text()).toBe('Add mailbox')
    expect(wrapper.text()).not.toContain('SMTP')
    await fillAccount(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(createAccount).toHaveBeenCalledWith({ name: 'Personal', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls', imap_username: 'me@example.test', password: 'imap-secret',mark_as_read:false,sync_folders:false,reply_from_recipient:false,save_sent:true,folder_mapping:{} })
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it('selects the corresponding default port when changing IMAP security', async () => {
    const wrapper = mount(AccountModal, { props: { modelValue: true } })
    await wrapper.find('select').setValue('starttls')
    expect((wrapper.find('input[type="number"]').element as HTMLInputElement).value).toBe('143')
    await wrapper.find('select').setValue('tls')
    expect((wrapper.find('input[type="number"]').element as HTMLInputElement).value).toBe('993')
  })

  it('keeps the form open and shows a failed save', async () => {
    vi.mocked(createAccount).mockRejectedValue(new Error('The file changed since it was read.'))
    const wrapper = mount(AccountModal, { props: { modelValue: true } })
    await fillAccount(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('.error-banner').text()).toContain('The file changed')
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.findAll('input')[0]!.element.value).toBe('Personal')
  })
})

const mappedAccount = {id:'work',name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret',is_default:true,sync_folders:true,folder_mapping:{trash:'Personal/Deleted'}}
const serverFolder = (name:string,selectable=true) => ({name,selectable,delimiter:'/',messages:0,unseen:0,uid_next:1,uid_validity:7})
describe('manual IMAP folder mapping', () => {
  it('saves and restores None independently from Automatic and a real folder named none',async()=>{
    const wrapper=mount(AccountModal,{props:{modelValue:true,account:{...mappedAccount,folder_mapping:{trash:null,sent:null}}}});await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('#folder-map-trash').element.value).toBe('none')
    await wrapper.get('#folder-map-sent').setValue('')
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(updateAccount).toHaveBeenCalledWith('work',expect.objectContaining({folder_mapping:{trash:null}}))
    await wrapper.setProps({modelValue:false});await wrapper.setProps({modelValue:true,account:mappedAccount});await flushPromises()
    vi.mocked(listFolders).mockResolvedValue({folders:[serverFolder('none')],truncated:false})
    await wrapper.get('.folder-mapping button').trigger('click');await flushPromises()
    await wrapper.get('#folder-map-trash').setValue('folder:none')
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(updateAccount).toHaveBeenLastCalledWith('work',expect.objectContaining({folder_mapping:{trash:'none'}}))
    wrapper.unmount()
  })
  it('loads only on request, keeps real paths and saves manual choices without mutating the account', async () => {
    vi.mocked(listFolders).mockResolvedValue({folders:[serverFolder('INBOX'),serverFolder('Personal/Deleted'),serverFolder('Envoyés & été'),serverFolder('Container',false)],truncated:false})
    const wrapper=mount(AccountModal,{props:{modelValue:true,account:mappedAccount}})
    await flushPromises()
    expect(listFolders).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLSelectElement>('#folder-map-trash').element.value).toBe('folder:Personal/Deleted')
    await wrapper.get('.folder-mapping button').trigger('click');await flushPromises()
    expect(listFolders).toHaveBeenCalledWith('work',expect.any(AbortSignal))
    expect(wrapper.get('#folder-map-sent').findAll('option').map(option=>option.element.value)).toEqual(['','none','folder:Envoyés & été','folder:Personal/Deleted'])
    await wrapper.get('#folder-map-sent').setValue('folder:Envoyés & été')
    expect(wrapper.get('#folder-map-archive').findAll('option').find(option=>option.element.value==='folder:Envoyés & été')!.attributes()).toHaveProperty('disabled')
    expect(mappedAccount.folder_mapping).toEqual({trash:'Personal/Deleted'})
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(updateAccount).toHaveBeenCalledWith('work',expect.objectContaining({folder_mapping:{trash:'Personal/Deleted',sent:'Envoyés & été'}}))
    wrapper.unmount()
  })
  it('preserves an unavailable saved mapping and lets the user reset it to Automatic', async () => {
    vi.mocked(listFolders).mockResolvedValue({folders:[serverFolder('Trash')],truncated:false})
    const wrapper=mount(AccountModal,{props:{modelValue:true,account:mappedAccount}});await flushPromises()
    await wrapper.get('.folder-mapping button').trigger('click');await flushPromises()
    expect(wrapper.get('#folder-map-trash').text()).toContain('Personal/Deleted (unavailable)')
    await wrapper.get('#folder-map-trash').setValue('')
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(updateAccount).toHaveBeenCalledWith('work',expect.objectContaining({folder_mapping:{}}))
    expect(mappedAccount.folder_mapping.trash).toBe('Personal/Deleted')
    wrapper.unmount()
  })
  it('keeps saved choices when loading fails and cancels late results on close', async () => {
    vi.mocked(listFolders).mockRejectedValueOnce(new Error('Provider unavailable'))
    const wrapper=mount(AccountModal,{props:{modelValue:true,account:mappedAccount}});await flushPromises()
    await wrapper.get('.folder-mapping button').trigger('click');await flushPromises()
    expect(wrapper.get('.folder-mapping [role="alert"]').text()).toBe('Provider unavailable')
    expect(wrapper.get<HTMLSelectElement>('#folder-map-trash').element.value).toBe('folder:Personal/Deleted')
    let finish!:(page:Awaited<ReturnType<typeof listFolders>>)=>void
    vi.mocked(listFolders).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
    await wrapper.get('.folder-mapping button').trigger('click')
    const signal=vi.mocked(listFolders).mock.calls[vi.mocked(listFolders).mock.calls.length-1]![1]
    await wrapper.setProps({modelValue:false});expect(signal.aborted).toBe(true)
    finish({folders:[serverFolder('Late private folder')],truncated:false});await flushPromises()
    await wrapper.setProps({modelValue:true,account:null})
    expect(wrapper.text()).not.toContain('Late private folder')
    expect(wrapper.text()).toContain('Save this mailbox first')
    wrapper.unmount()
  })
  it('requires saving edited connection credentials before using the saved mailbox folders', async () => {
    const wrapper=mount(AccountModal,{props:{modelValue:true,account:mappedAccount}});await flushPromises()
    await wrapper.findAll('input')[1]!.setValue('another.example.test')
    expect(wrapper.text()).toContain('Save your connection changes')
    expect(wrapper.find('.folder-mapping button').exists()).toBe(false)
    expect(wrapper.get('#folder-map-trash').attributes()).toHaveProperty('disabled')
    expect(listFolders).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
