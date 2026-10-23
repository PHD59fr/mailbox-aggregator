import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ComposeView from './ComposeView.vue'
import RichTextEditor from '../components/RichTextEditor.vue'
import { listAccounts, listSMTPConfigs } from '../api/accounts'
import { sendMessage, type SendResult } from '../api/messages'
import { api } from '../api/client'
import { useSessionStore } from '../stores/session'
import { useMailActions, type MailAction } from '../stores/mailActions'
import type { Message } from '../api/reading'

const draftStore = vi.hoisted(() => ({ records: new Map<string, unknown>() }))

vi.mock('../api/accounts', () => ({ listSMTPConfigs: vi.fn(),listAccounts:vi.fn() }))
vi.mock('../api/messages', () => ({ sendMessage: vi.fn() }))
vi.mock('../api/client', async importOriginal => ({ ...await importOriginal<typeof import('../api/client')>(), api: vi.fn() }))
vi.mock('../crypto/draftStorage', () => ({
  listStoredDrafts: async () => [...draftStore.records.values()],
  replaceStoredDraft: async (previous: unknown, record: unknown) => { const id=(record as {id:string}).id; if(JSON.stringify(draftStore.records.get(id))===JSON.stringify(previous)) draftStore.records.set(id,record) },
  getStoredDraft: async (id: string) => draftStore.records.get(id),
  putStoredDraft: async (record: unknown) => { draftStore.records.set((record as { id: string }).id, record) },
  deleteStoredDraft: async (id: string) => { draftStore.records.delete(id) },
  pruneStoredDrafts: async () => {},
}))

const config = { id: 'send-only', name: 'Support', host: 'smtp.example.test', port: 465, security: 'tls' as const, username: 'smtp-user', password: 'secret', sender_name: 'Support', sender_email: 'support@example.test', is_default: true }
const received:Message = {uid:1,uid_validity:7,subject:'Incoming',from:[{email:'writer@example.test'}],to:[{email:'alias@example.test'}],cc:[],date:'2026-10-22T12:00:00Z',seen:true,size:100,text:'Hello',html:'',attachments:[],truncated:false}
function prepareReply(action:MailAction='reply',replyFromRecipient=true,message=received) {
  useMailActions().pending={action,reference:{accountID:'imap-work',uid:1,validity:7},message,smtpConfigID:config.id,replyFromRecipient}
}
let router: Router
beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
  draftStore.records.clear()
  sessionStorage.clear()
  vi.stubGlobal('indexedDB', {})
  useSessionStore().$patch({ authenticated: true, userID: 'test-user', vmk: new Uint8Array(32).fill(7) })
  vi.mocked(listSMTPConfigs).mockResolvedValue({ [config.id]: config })
  vi.mocked(listAccounts).mockResolvedValue({})
  vi.mocked(sendMessage).mockResolvedValue({ status: 'sent', message_id: '<test@example.test>' })
  vi.mocked(api).mockResolvedValue(undefined as never)
})
afterEach(() => { vi.unstubAllGlobals() })

async function openComposer(smtpConfigID?: string) {
  router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/compose', component: { template: '<div />' } },
    { path: '/settings', component: { template: '<div />' } },
    { path: '/mail', component: { template: '<div />' } },
  ] })
  await router.push('/compose')
  const wrapper = mount(ComposeView, { props: { smtpConfigID }, global: { plugins: [router], stubs: { AppHeader: true, VaultUnlock: true } } })
  await flushPromises()
  return wrapper
}

async function fillMessage(wrapper: Awaited<ReturnType<typeof openComposer>>) {
  await wrapper.findAll('input[type="email"]')[0]!.setValue('one@example.test, two@example.test')
  await wrapper.find('input[maxlength="998"]').setValue('Hello')
  wrapper.findComponent(RichTextEditor).vm.$emit('update:modelValue', '<p>SMTP only</p>')
  wrapper.findComponent(RichTextEditor).vm.$emit('update:text', 'SMTP only')
  await wrapper.vm.$nextTick()
}

describe('compose with a sending identity only', () => {
  it('selects one linked mailbox for Sent, respects None, and reports an accepted message separately from a failed archive',async()=>{
    const account={id:'work',name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls' as const,imap_username:'own',is_default:true,smtp_config_id:config.id,folder_mapping:{sent:'Work/Sent'},save_sent:true}
    vi.mocked(listAccounts).mockResolvedValue({work:account,disabled:{...account,id:'disabled',name:'Disabled',folder_mapping:{sent:null}}})
    vi.mocked(sendMessage).mockResolvedValue({status:'sent',message_id:'<sent@example.test>',sent_copy:{status:'failed',message:'Message sent, but its Sent copy could not be saved.'}})
    const wrapper=await openComposer()
    expect((wrapper.get('#compose-sent-copy').element as HTMLSelectElement).value).toBe('work')
    expect(wrapper.get('#compose-sent-copy').text()).not.toContain('Disabled')
    await fillMessage(wrapper);await wrapper.get('form').trigger('submit');await flushPromises()
    expect(sendMessage).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({sent_account_id:'work'}),'')
    expect(router.currentRoute.value.path).toBe('/mail');expect(useSessionStore().noticeKind).toBe('warning')
    expect(useSessionStore().notice).toContain('Message sent, but');wrapper.unmount()
  })
  it('allows skipping the Sent copy for a single message without changing the linked identity',async()=>{
    vi.mocked(listAccounts).mockResolvedValue({work:{id:'work',name:'Work',imap_host:'imap.example.test',imap_port:993,imap_security:'tls',imap_username:'own',is_default:true,smtp_config_id:config.id}})
    const wrapper=await openComposer();await wrapper.get('#compose-sent-copy').setValue('')
    await fillMessage(wrapper);await wrapper.get('form').trigger('submit');await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.not.objectContaining({sent_account_id:expect.anything()}),'');wrapper.unmount()
  })
  it.each(['reply','reply-all'] as const)('prefers the matching recipient sender for %s and allows changing it before sending', async action => {
    const alias={...config,id:'alias',sender_email:'alias@example.test',is_default:false}
    vi.mocked(listSMTPConfigs).mockResolvedValue({[config.id]:config,alias})
    prepareReply(action)
    const wrapper=await openComposer(config.id)
    expect((wrapper.find('#compose-from').element as HTMLSelectElement).value).toBe('alias')
    expect((wrapper.find('#compose-to').element as HTMLInputElement).value).toBe('writer@example.test')
    await wrapper.find('#compose-from').setValue(config.id)
    await wrapper.find('form').trigger('submit');await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({smtp_config_id:config.id}), '')
    expect(useMailActions().pending).toBeNull();wrapper.unmount()
  })
  it('falls back to the first sender rather than the linked/default sender when no recipient matches', async () => {
    const first={...config,id:'first',sender_email:'first@example.test',is_default:false}
    vi.mocked(listSMTPConfigs).mockResolvedValue({first,[config.id]:config})
    prepareReply()
    const wrapper=await openComposer(config.id)
    expect((wrapper.find('#compose-from').element as HTMLSelectElement).value).toBe('first');wrapper.unmount()
  })
  it.each([['reply',false],['reply-all',false],['forward',true],['redirect',true]] as const)('retains linked sender selection for %s with recipient preference %s', async (action,enabled) => {
    const alias={...config,id:'alias',sender_email:'alias@example.test',is_default:false}
    vi.mocked(listSMTPConfigs).mockResolvedValue({alias,[config.id]:config})
    prepareReply(action,enabled)
    const wrapper=await openComposer()
    expect((wrapper.find('#compose-from').element as HTMLSelectElement).value).toBe(config.id);wrapper.unmount()
  })
  it('preselects a linked sending identity even when another identity is the default', async () => {
    const linked = { ...config, id: 'linked', sender_email: 'linked@example.test', is_default: false }
    vi.mocked(listSMTPConfigs).mockResolvedValue({ [config.id]: config, [linked.id]: linked })
    const wrapper = await openComposer(linked.id)
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe(linked.id)
    expect(wrapper.find('select option:checked').text()).toContain(linked.sender_email)
  })
  it('sends with no IMAP dependency and clears the draft after acceptance', async () => {
    const wrapper = await openComposer()
    expect(router.currentRoute.value.path).toBe('/compose')
    expect(wrapper.find('select option').text()).toContain(config.sender_email)
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith({ smtp_config_id: config.id, to: [{ email: 'one@example.test' }, { email: 'two@example.test' }], cc: [], bcc: [], subject: 'Hello', text: 'SMTP only', html: '<p>SMTP only</p>', attachments: [] }, '')
    expect(router.currentRoute.value.path).toBe('/mail')
    expect(useSessionStore().notice).toBe('Message sent.')
    expect(wrapper.findComponent(RichTextEditor).props('modelValue')).toBe('')
  })

  it('preserves the draft after a failed send without retrying', async () => {
    vi.mocked(sendMessage).mockRejectedValue(new Error('Delivery could not be confirmed.'))
    const wrapper = await openComposer()
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('could not be confirmed')
    expect(wrapper.findComponent(RichTextEditor).props('modelValue')).toBe('<p>SMTP only</p>')
    expect(sendMessage).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/compose')
    expect(useSessionStore().notice).toBe('')
  })

  it('prevents duplicate submits while an SMTP request is pending', async () => {
    vi.mocked(sendMessage).mockImplementation(() => new Promise(() => {}))
    const wrapper = await openComposer()
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('fieldset').attributes()).toHaveProperty('disabled')
    expect(sendMessage).toHaveBeenCalledTimes(1)
  })

  it('shows only To by default and reveals Cc and Bcc independently when requested', async () => {
    const wrapper = await openComposer()
    expect(wrapper.find('#compose-to').exists()).toBe(true)
    expect(wrapper.find('#compose-cc').exists()).toBe(false)
    expect(wrapper.find('#compose-bcc').exists()).toBe(false)
    await wrapper.find('[aria-controls="compose-bcc"]').trigger('click')
    expect(wrapper.find('#compose-cc').exists()).toBe(false)
    await wrapper.find('[aria-controls="compose-cc"]').trigger('click')
    await wrapper.find('#compose-bcc').setValue('hidden@example.test')
    await wrapper.find('#compose-cc').setValue('copy@example.test')
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ cc: [{ email: 'copy@example.test' }], bcc: [{ email: 'hidden@example.test' }] }), '')
  })

  it('clears Cc recipients when their row is removed and can show it again', async () => {
    const wrapper = await openComposer()
    await wrapper.find('[aria-controls="compose-cc"]').trigger('click')
    await wrapper.find('#compose-cc').setValue('copy@example.test')
    await wrapper.find('[aria-label="Remove Cc"]').trigger('click')
    expect(wrapper.find('#compose-cc').exists()).toBe(false)
    await wrapper.find('[aria-controls="compose-cc"]').trigger('click')
    expect((wrapper.find('#compose-cc').element as HTMLInputElement).value).toBe('')
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ cc: [] }), '')
  })

  it('clears hidden Bcc recipients when the Bcc row is removed', async () => {
    const wrapper = await openComposer()
    await wrapper.find('[aria-controls="compose-bcc"]').trigger('click')
    await wrapper.find('#compose-bcc').setValue('hidden@example.test')
    await wrapper.find('[aria-label="Remove Bcc"]').trigger('click')
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ bcc: [] }), '')
  })

  it('attaches files, supports removal and preserves them after SMTP failure', async () => {
    vi.mocked(sendMessage).mockRejectedValue(new Error('SMTP unavailable'))
    const wrapper = await openComposer()
    const file = new File(['hello'], 'report.txt', { type: 'text/plain' })
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    expect(wrapper.find('.compose-attachments').text()).toContain('report.txt')
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(sendMessage).toHaveBeenCalled())
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({ attachments: [{ filename: 'report.txt', content_type: 'text/plain', data: 'aGVsbG8=' }] }), '')
    expect(wrapper.find('.compose-attachments').text()).toContain('report.txt')
    expect(router.currentRoute.value.path).toBe('/compose')
    await wrapper.find('[aria-label="Remove attachment report.txt"]').trigger('click')
    expect(wrapper.find('.compose-attachments').exists()).toBe(false)
  })

  it('rejects oversized attachments without losing already selected files', async () => {
    const wrapper = await openComposer()
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['small'], 'keep.txt')], configurable: true })
    await input.trigger('change')
    Object.defineProperty(input.element, 'files', { value: [new File([new Uint8Array(10 * 1024 * 1024)], 'too-big.bin')], configurable: true })
    await input.trigger('change')
    expect(wrapper.find('[role="alert"]').text()).toContain('10 MB')
    expect(wrapper.find('.compose-attachments').text()).toContain('keep.txt')
    expect(wrapper.find('.compose-attachments').text()).not.toContain('too-big.bin')
    expect(sendMessage).not.toHaveBeenCalled()
  })

  it('redirects to the SMTP settings when no sender exists', async () => {
    vi.mocked(listSMTPConfigs).mockResolvedValue({})
    const wrapper = await openComposer()
    expect(router.currentRoute.value.fullPath).toBe('/settings?add=sender#identities')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('does not mistake a failed SMTP configuration load for an empty configuration', async () => {
    vi.mocked(listSMTPConfigs).mockRejectedValue(new Error('Vault could not be read.'))
    const wrapper = await openComposer()
    expect(router.currentRoute.value.path).toBe('/compose')
    expect(wrapper.find('[role="alert"]').text()).toContain('Vault could not be read.')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('waits until the vault is unlocked before checking SMTP settings', async () => {
    const session = useSessionStore()
    session.lockLocal()
    vi.mocked(listSMTPConfigs).mockResolvedValue({})
    const wrapper = await openComposer()
    expect(listSMTPConfigs).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/compose')
    session.$patch({ vmk: new Uint8Array(32).fill(7) })
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/settings?add=sender#identities')
    wrapper.unmount()
  })

  it('ignores an empty SMTP response if the vault was locked during loading', async () => {
    let resolve!: (configs: Record<string, typeof config>) => void
    vi.mocked(listSMTPConfigs).mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = await openComposer()
    useSessionStore().lockLocal()
    resolve({})
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/compose')
    wrapper.unmount()
  })
})

describe('encrypted compose drafts', () => {
  async function attach(wrapper: Awaited<ReturnType<typeof openComposer>>, file: File) {
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
  }

  function storedText() { return JSON.stringify([...draftStore.records.values()]) }

  it('saves an encrypted draft before navigation and restores it in a fresh composer', async () => {
    const wrapper = await openComposer()
    await fillMessage(wrapper)
    await attach(wrapper, new File(['attachment secret'], 'notes.txt', { type: 'text/plain' }))
    await vi.waitFor(() => {
      const records = [...draftStore.records.values()] as Array<{ attachments: unknown[] }>
      expect(records[0]?.attachments).toHaveLength(1)
    })
    const saved = storedText()
    expect(saved).not.toContain('one@example.test')
    expect(saved).not.toContain('Hello')
    expect(saved).not.toContain('SMTP only')
    expect(saved).not.toContain('attachment secret')

    await router.push('/mail')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/mail')
    wrapper.unmount()

    const restored = await openComposer()
    await vi.waitFor(() => {
      expect((restored.find('#compose-subject').element as HTMLInputElement).value).toBe('Hello')
    })
    expect((restored.find('#compose-to').element as HTMLInputElement).value).toBe('one@example.test,two@example.test')
    expect(restored.findComponent(RichTextEditor).props('modelValue')).toBe('<p>SMTP only</p>')
    expect(restored.find('.compose-attachments').text()).toContain('notes.txt')
    expect(restored.find('.compose-draft-restored').text()).toContain('Draft restored')

    await restored.find('form').trigger('submit')
    await vi.waitFor(() => expect(sendMessage).toHaveBeenCalled())
    await flushPromises()
    expect(sendMessage).toHaveBeenCalledWith(expect.objectContaining({
      to: [{ email: 'one@example.test' }, { email: 'two@example.test' }],
      subject: 'Hello',
      attachments: [expect.objectContaining({ filename: 'notes.txt' })],
    }), '')
    expect(draftStore.records.size).toBe(0)
    expect(router.currentRoute.value.path).toBe('/mail')
    restored.unmount()
  })

  it('flushes and restores the draft across a vault lock without exposing its plaintext', async () => {
    const wrapper = await openComposer()
    await fillMessage(wrapper)
    await attach(wrapper, new File(['attachment secret'], 'notes.txt', { type: 'text/plain' }))
    const key = new Uint8Array(useSessionStore().vmk!)

    await useSessionStore().lock()
    await flushPromises()
    expect(useSessionStore().unlocked).toBe(false)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('SMTP only')
    const saved = storedText()
    expect(saved).not.toContain('one@example.test')
    expect(saved).not.toContain('Hello')
    expect(saved).not.toContain('SMTP only')
    expect(saved).not.toContain('attachment secret')
    expect([...draftStore.records.values()]).not.toHaveLength(0)

    useSessionStore().$patch({ vmk: key })
    await vi.waitFor(() => {
      expect((wrapper.find('#compose-subject').element as HTMLInputElement).value).toBe('Hello')
    })
    expect((wrapper.find('#compose-to').element as HTMLInputElement).value).toBe('one@example.test,two@example.test')
    expect(wrapper.findComponent(RichTextEditor).props('modelValue')).toBe('<p>SMTP only</p>')
    expect(wrapper.find('.compose-attachments').text()).toContain('notes.txt')
    expect(wrapper.find('.compose-draft-restored').exists()).toBe(true)
    wrapper.unmount()
  })
})

describe('sends already in progress when the composer disappears', () => {
  async function startSend(wrapper: Awaited<ReturnType<typeof openComposer>>) {
    let resolve!: (result: SendResult) => void
    let reject!: (error: Error) => void
    vi.mocked(sendMessage).mockImplementation(() => new Promise<SendResult>((done, fail) => { resolve = done; reject = fail }))
    await fillMessage(wrapper)
    await wrapper.find('form').trigger('submit')
    await vi.waitFor(() => expect(sendMessage).toHaveBeenCalled())
    await vi.waitFor(() => expect(draftStore.records.size).toBe(1))
    return { resolve, reject }
  }

  it('keeps an accepted send running after navigation and clears its encrypted draft', async () => {
    const wrapper = await openComposer()
    const { resolve } = await startSend(wrapper)
    await router.push('/mail')
    wrapper.unmount()

    resolve({ status: 'sent', message_id: '<sent@example.test>' })
    await flushPromises()
    expect(useSessionStore().notice).toBe('Message sent.')
    expect(useSessionStore().noticeKind).toBe('success')
    expect(useSessionStore().sentDraftID).not.toBe('')
    expect(draftStore.records.size).toBe(0)
  })

  it('defers the accepted outcome across a vault lock and clears the draft', async () => {
    const wrapper = await openComposer()
    const { resolve } = await startSend(wrapper)
    await useSessionStore().lock()
    await flushPromises()
    expect(wrapper.find('form').exists()).toBe(false)
    expect(draftStore.records.size).toBe(1)

    resolve({ status: 'sent', message_id: '<locked@example.test>' })
    await flushPromises()
    expect(useSessionStore().notice).toBe('')
    expect(useSessionStore().deferredNotice).toBe('Message sent.')
    expect(useSessionStore().deferredNoticeKind).toBe('success')
    expect(draftStore.records.size).toBe(0)

    useSessionStore().$patch({ vmk: new Uint8Array(32).fill(7) })
    useSessionStore().flushDeferredNotice()
    expect(useSessionStore().notice).toBe('Message sent.')
    expect(useSessionStore().deferredNotice).toBe('')
    wrapper.unmount()
  })

  it('keeps the draft and warns when a send fails after the composer is gone', async () => {
    const wrapper = await openComposer()
    const { reject } = await startSend(wrapper)
    await router.push('/mail')
    wrapper.unmount()

    reject(new Error('Delivery could not be confirmed.'))
    await flushPromises()
    expect(useSessionStore().notice).toContain('Delivery was not confirmed.')
    expect(useSessionStore().noticeKind).toBe('warning')
    expect(draftStore.records.size).toBe(1)
  })

  it('discards a restored draft that was already accepted by a late send', async () => {
    const wrapper = await openComposer()
    await fillMessage(wrapper)
    await vi.waitFor(() => expect(draftStore.records.size).toBe(1))
    const id = sessionStorage.getItem('mailbox-compose-draft-id-v1')
    expect(id).toBeTruthy()

    useSessionStore().sentDraftID = id!
    await vi.waitFor(() => {
      expect((wrapper.find('#compose-subject').element as HTMLInputElement).value).toBe('')
    })
    expect(draftStore.records.size).toBe(0)
    expect(useSessionStore().sentDraftID).toBe('')
    wrapper.unmount()
  })
})

it('disables a restored draft while its earlier SMTP submission is pending and prevents a second send',async()=>{
 let resolve!:(result:SendResult)=>void
 vi.mocked(sendMessage).mockImplementation(()=>new Promise(done=>{resolve=done}))
 const first=await openComposer();await fillMessage(first)
 await first.find('form').trigger('submit');await vi.waitFor(()=>expect(sendMessage).toHaveBeenCalledTimes(1))
 await router.push('/mail');first.unmount()
 const restored=await openComposer()
 await vi.waitFor(()=>expect((restored.get('#compose-subject').element as HTMLInputElement).value).toBe('Hello'))
 expect(restored.get('fieldset').attributes()).toHaveProperty('disabled')
 expect(restored.text()).toContain('still being sent')
 await restored.find('form').trigger('submit');expect(sendMessage).toHaveBeenCalledTimes(1)
 resolve({status:'sent',message_id:'<accepted@example.test>'});await flushPromises()
 expect((restored.get('#compose-subject').element as HTMLInputElement).value).toBe('')
 expect(useSessionStore().pendingDraftIDs).toEqual([])
 restored.unmount()
})

it('still locks and preserves a warning when the final draft flush runs out of browser storage',async()=>{
 const wrapper=await openComposer();await fillMessage(wrapper)
 const session=useSessionStore();const key=session.vmk!
 const write=vi.spyOn(draftStore.records,'set').mockImplementation(()=>{throw new DOMException('Storage quota exceeded','QuotaExceededError')})
 try{
  await session.lock();await flushPromises()
  expect(session.unlocked).toBe(false)
  expect(key.every(byte=>byte===0)).toBe(true)
  expect(session.deferredNotice).toContain('could not be saved before locking')
  expect(session.deferredNoticeKind).toBe('warning')
 }finally{write.mockRestore();wrapper.unmount()}
})
