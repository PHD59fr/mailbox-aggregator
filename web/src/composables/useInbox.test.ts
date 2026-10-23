import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInbox } from './useInbox'
import { listFolders, listMessages, readMessage, markMessageSeen, trashMessage, type Message } from '../api/reading'
import { APIError } from '../api/client'
import { useSessionStore } from '../stores/session'
import { useMailboxStore } from '../stores/mailboxes'

vi.mock('../api/reading', () => ({ listFolders:vi.fn(),listMessages: vi.fn(), readMessage: vi.fn(), downloadAttachment: vi.fn(),markMessageSeen:vi.fn(),trashMessage:vi.fn() }))
const account = { id: 'work', name: 'Work', imap_host: 'imap.example.test', imap_port: 993, imap_security: 'tls' as const, imap_username: 'me', password: 'secret', is_default: true }
const row = { uid: 50, subject: 'Hello', from: [{ email: 'team@example.test' }], to: [], cc: [], date: '2026-10-22T12:00:00Z', seen: false, size: 100 }
let inbox: ReturnType<typeof useInbox>
const Inbox = defineComponent({ setup() { inbox = useInbox(); return () => null } })
beforeEach(() => {
  vi.resetAllMocks(); setActivePinia(createPinia())
  Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true})
  useSessionStore().$patch({ authenticated: true, userID: 'user', vmk: new Uint8Array(32), loading: false })
  useMailboxStore().accounts = [{...account}]
  vi.mocked(listMessages).mockResolvedValue({ messages: [{...row}], uid_validity: 7, next_before: 50, total: 55984 })
  vi.mocked(markMessageSeen).mockResolvedValue(undefined)
  vi.mocked(readMessage).mockResolvedValue({ ...row, uid_validity: 7, text: 'Private content', html: '', attachments: [], truncated: false })
})
afterEach(() => vi.useRealTimers())

describe('inbox and reader state', () => {
  it('moves the selected message to Trash without retrying or restarting monitoring',async()=>{
    const wrapper=mount(Inbox);await flushPromises();await inbox.open(inbox.rows.value[0]!)
    vi.mocked(trashMessage).mockResolvedValue(undefined)
    await inbox.removeMessage()
    expect(trashMessage).toHaveBeenCalledExactlyOnceWith({accountID:'work',uid:50,validity:7},expect.any(AbortSignal))
    expect(inbox.rows.value).toEqual([]);expect(inbox.selected.value).toBeNull();expect(useMailboxStore().refreshToken).toBe(0)
    expect(useSessionStore().notice).toBe('Message moved to Trash.');wrapper.unmount();useSessionStore().dismissNotice()
  })
  it('keeps the message open on an unconfirmed deletion and never retries it automatically',async()=>{
    vi.useFakeTimers();const wrapper=mount(Inbox);await flushPromises();await inbox.open(inbox.rows.value[0]!)
    vi.mocked(trashMessage).mockRejectedValue(new APIError('IMAP_TRASH_UNCONFIRMED','Refresh before trying again.',502))
    await inbox.removeMessage();await vi.advanceTimersByTimeAsync(60000)
    expect(trashMessage).toHaveBeenCalledTimes(1);expect(inbox.rows.value).toHaveLength(1);expect(inbox.message.value).not.toBeNull();expect(inbox.readError.value).toContain('Refresh');wrapper.unmount()
  })
  it('debounces server-side searches, combines filters and clears them on request',async()=>{
    vi.useFakeTimers();const wrapper=mount(Inbox);await flushPromises()
    const mailbox=useMailboxStore();mailbox.search='old message';mailbox.readFilter='unread';mailbox.senderFilter='team@example.test';mailbox.recipientFilter='me@example.test';mailbox.subjectFilter='Invoice';mailbox.sinceFilter='2026-10-01';mailbox.untilFilter='2026-10-31'
    await flushPromises();await vi.advanceTimersByTimeAsync(399);expect(listMessages).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'INBOX',{search:'old message',seen:'unread',from:'team@example.test',to:'me@example.test',subject:'Invoice',since:'2026-10-01',until:'2026-10-31'})
    expect(mailbox.filterCount).toBe(6)
    mailbox.clearFilters();await flushPromises();await vi.advanceTimersByTimeAsync(400)
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0);expect(mailbox.filterCount).toBe(0);wrapper.unmount()
  })
  it('does not let a late search result overwrite the current search',async()=>{
    vi.useFakeTimers();const wrapper=mount(Inbox);await flushPromises()
    let resolve!: (page:Awaited<ReturnType<typeof listMessages>>)=>void
    vi.mocked(listMessages).mockImplementationOnce(()=>new Promise(done=>{resolve=done}))
    useMailboxStore().search='first';await flushPromises();await vi.advanceTimersByTimeAsync(400)
    const signal=vi.mocked(listMessages).mock.calls[1]![1]
    useMailboxStore().search='second';await flushPromises();expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(400);resolve({messages:[],uid_validity:7,total:0,next_before:0});await flushPromises()
    expect(inbox.rows.value).toHaveLength(1);wrapper.unmount()
  })
  it('preserves existing rows on refresh and replaces an in-flight list safely', async () => {
    const wrapper = mount(Inbox); await flushPromises()
    let resolve!: (page: Awaited<ReturnType<typeof listMessages>>) => void
    vi.mocked(listMessages).mockImplementationOnce(() => new Promise(done => {resolve=done}))
    inbox.reload(); await flushPromises()
    const signal = vi.mocked(listMessages).mock.calls[1]![1]
    expect(inbox.rows.value).toHaveLength(1)
    inbox.reload(); await flushPromises()
    expect(signal.aborted).toBe(true); expect(listMessages).toHaveBeenCalledTimes(3); expect(inbox.loading.value).toBe(false)
    resolve({messages:[],uid_validity:7,next_before:0,total:0}); await flushPromises()
    expect(inbox.rows.value).toHaveLength(1); wrapper.unmount()
  })
  it('retries pagination with the original UID anchor and pauses retries while hidden', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Inbox); await flushPromises()
    vi.mocked(listMessages).mockRejectedValueOnce(new TypeError('Network error'))
    await inbox.loadMore(); await vi.advanceTimersByTimeAsync(3000)
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),50,7)
    vi.mocked(listMessages).mockRejectedValue(new TypeError('Offline'))
    inbox.reload(); await flushPromises(); const calls = vi.mocked(listMessages).mock.calls.length
    Object.defineProperty(document,'visibilityState',{value:'hidden',configurable:true})
    document.dispatchEvent(new Event('visibilitychange')); await vi.advanceTimersByTimeAsync(30000)
    expect(listMessages).toHaveBeenCalledTimes(calls)
    Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true})
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(listMessages).toHaveBeenCalledTimes(calls+1); wrapper.unmount()
  })
  it('retries transient DNS failures automatically without a status change', async () => {
    vi.useFakeTimers()
    vi.mocked(listMessages).mockRejectedValueOnce(new APIError('MAIL_SERVER_UNAVAILABLE','Mail server could not be resolved.',503))
    const wrapper = mount(Inbox); await flushPromises()
    expect(inbox.errors.value.join('')).toContain('Retrying automatically')
    await vi.advanceTimersByTimeAsync(3000)
    expect(listMessages).toHaveBeenCalledTimes(2); expect(inbox.rows.value).toHaveLength(1); expect(inbox.errors.value).toEqual([])
    wrapper.unmount()
  })
  it('backs off failed reads, stops on lock and never retries invalid credentials', async () => {
    vi.useFakeTimers()
    vi.mocked(listMessages).mockRejectedValue(new TypeError('Failed to fetch'))
    const wrapper = mount(Inbox); await flushPromises()
    await vi.advanceTimersByTimeAsync(3000); expect(listMessages).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5999); expect(listMessages).toHaveBeenCalledTimes(2)
    useSessionStore().lockLocal(); await flushPromises()
    await vi.advanceTimersByTimeAsync(60000); expect(listMessages).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    useSessionStore().vmk = new Uint8Array(32)
    vi.mocked(listMessages).mockRejectedValue(new APIError('IMAP_AUTH_FAILED','Wrong password',422))
    const invalid = mount(Inbox); await flushPromises(); const calls = vi.mocked(listMessages).mock.calls.length
    await vi.advanceTimersByTimeAsync(60000); expect(listMessages).toHaveBeenCalledTimes(calls)
    invalid.unmount()
  })
  it('marking unread keeps the reader and does not restart monitoring or double-refresh', async () => {
    vi.useFakeTimers()
    const mailbox = useMailboxStore()
    mailbox.statuses.work = {account_id:'work',uid_validity:7,uid_next:51,messages:1,unseen:0,mode:'idle'}
    vi.mocked(readMessage).mockResolvedValue({...row,seen:true,uid_validity:7,text:'Hello',html:'',attachments:[],truncated:false})
    const wrapper = mount(Inbox); await flushPromises(); await inbox.open(inbox.rows.value[0]!)
    await inbox.toggleSeen(); await flushPromises()
    expect(mailbox.refreshToken).toBe(0); expect(listMessages).toHaveBeenCalledTimes(1)
    mailbox.statuses.work = {...mailbox.statuses.work!,unseen:1}; await flushPromises()
    await vi.advanceTimersByTimeAsync(750)
    expect(listMessages).toHaveBeenCalledTimes(2); expect(readMessage).toHaveBeenCalledTimes(1)
    expect(inbox.message.value?.seen).toBe(false)
    wrapper.unmount()
  })
  it('loads folders only when opted in, and keeps identical UIDs scoped to their folder', async () => {
    useMailboxStore().accounts[0]!.sync_folders = true
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Sent/2026',delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:51,uid_validity:7}],truncated:false})
    const wrapper = mount(Inbox); await flushPromises()
    expect(listFolders).toHaveBeenCalledTimes(1)
    inbox.chooseMailbox('work','Sent/2026'); await flushPromises()
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'Sent/2026')
    await inbox.open(inbox.rows.value[0]!)
    expect(inbox.selected.value).toEqual({accountID:'work',uid:50,validity:7,folder:'Sent/2026'})
    await inbox.toggleSeen()
    expect(markMessageSeen).toHaveBeenLastCalledWith(expect.objectContaining({folder:'Sent/2026'}),expect.any(AbortSignal),true)
    wrapper.unmount()
  })
  it('marks opened mail only when configured, and supports reversing it manually', async () => {
    useMailboxStore().accounts[0]!.mark_as_read = true
    const wrapper = mount(Inbox); await flushPromises(); await inbox.open(inbox.rows.value[0]!)
    expect(markMessageSeen).toHaveBeenCalledTimes(1); expect(inbox.message.value?.seen).toBe(true)
    await inbox.toggleSeen()
    expect(markMessageSeen).toHaveBeenLastCalledWith({accountID:'work',uid:50,validity:7},expect.any(AbortSignal),false)
    expect(inbox.message.value?.seen).toBe(false); wrapper.unmount()
  })
  it('keeps mail unread without automatic marking and offers manual mark-as-read', async () => {
    const wrapper = mount(Inbox); await flushPromises(); await inbox.open(inbox.rows.value[0]!)
    expect(markMessageSeen).not.toHaveBeenCalled()
    await inbox.toggleSeen(); expect(markMessageSeen).toHaveBeenLastCalledWith(expect.anything(),expect.any(AbortSignal),true)
    expect(inbox.message.value?.seen).toBe(true); wrapper.unmount()
  })
  it('loads summaries first, then fetches content only on opening', async () => {
    const wrapper = mount(Inbox); await flushPromises()
    expect(inbox.rows.value).toHaveLength(1)
    expect(readMessage).not.toHaveBeenCalled()
    await inbox.open(inbox.rows.value[0]!)
    expect(readMessage).toHaveBeenCalledWith({ accountID: 'work', uid: 50, validity: 7 },expect.any(AbortSignal))
    expect(inbox.message.value?.text).toBe('Private content')
    wrapper.unmount()
  })
  it('paginates by UID and prevents duplicate rows', async () => {
    const wrapper = mount(Inbox); await flushPromises()
    vi.mocked(listMessages).mockResolvedValue({ messages: [row, { ...row, uid: 49 }], uid_validity: 7, next_before: 0, total: 55984 })
    await inbox.loadMore()
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),50,7)
    expect(inbox.rows.value.map(row => row.uid)).toEqual([50,49])
    expect(inbox.hasMore.value).toBe(false)
    wrapper.unmount()
  })
  it('aborts reading and clears all message data on lock, including late responses', async () => {
    const wrapper = mount(Inbox); await flushPromises()
    let resolve!: (message: Message) => void
    vi.mocked(readMessage).mockImplementation(() => new Promise(done => { resolve = done }))
    const opening = inbox.open(inbox.rows.value[0]!)
    const signal = vi.mocked(readMessage).mock.calls[0]![1]
    useSessionStore().lockLocal(); await flushPromises()
    expect(signal.aborted).toBe(true)
    resolve({ ...row, uid_validity: 7, text: 'Late private content', html: '', attachments: [], truncated: false }); await opening
    expect(inbox.rows.value).toEqual([])
    expect(inbox.message.value).toBeNull()
    expect(inbox.selected.value).toBeNull()
    wrapper.unmount()
  })
  it('keeps a working mailbox visible if another mailbox fails', async () => {
    useMailboxStore().accounts.push({ ...account, id: 'other', name: 'Other' })
    vi.mocked(listMessages).mockImplementation(async accountID => {
      if (accountID === 'other') throw new Error('Connection failed')
      return { messages: [row], uid_validity: 7, next_before: 0, total: 1 }
    })
    const wrapper = mount(Inbox); await flushPromises()
    expect(inbox.rows.value).toHaveLength(1)
    expect(inbox.errors.value).toEqual(['Other: Connection failed'])
    inbox.chooseMailbox('work'); await flushPromises()
    expect(inbox.errors.value).toEqual([])
    wrapper.unmount()
  })
  it('refreshes the list on a live arrival without fetching every message body', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Inbox); await flushPromises()
    useMailboxStore().statuses.work = { account_id: 'work', uid_validity: 7, uid_next: 52, messages: 55985, unseen: 2, mode: 'idle' }
    await flushPromises(); await vi.advanceTimersByTimeAsync(750)
    expect(listMessages).toHaveBeenCalledTimes(2)
    expect(readMessage).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
