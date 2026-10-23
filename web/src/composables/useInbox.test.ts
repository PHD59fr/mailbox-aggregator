import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useInbox } from './useInbox'
import { listFolders, listMessages,listMessageReferences, readMessage, markMessageSeen, trashMessage, type Message } from '../api/reading'
import { APIError } from '../api/client'
import { useSessionStore } from '../stores/session'
import {useMailOrganization} from '../stores/organization'
import {messageKey} from '../api/mailSources'
import { useMailboxStore } from '../stores/mailboxes'

vi.mock('../api/reading', () => ({ listMessageReferences:vi.fn(),listFolders:vi.fn(),listMessages: vi.fn(), readMessage: vi.fn(), downloadAttachment: vi.fn(),markMessageSeen:vi.fn(),trashMessage:vi.fn() }))
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
  it('loads old tagged references by exact folder/UIDVALIDITY without scanning recent messages',async()=>{
    const reference={accountID:'work',folder:'Archive',validity:7,uid:1},organization=useMailOrganization()
    organization.data.entries[messageKey(reference)]={reference,tags:['billing']};organization.loaded=true
    vi.mocked(listMessageReferences).mockResolvedValue({messages:[{...row,uid:1}],uid_validity:7,next_before:0,total:1})
    const wrapper=mount(Inbox);await flushPromises();vi.mocked(listMessages).mockClear()
    useMailboxStore().tagFilter='billing';inbox.chooseView('tagged');await flushPromises()
    expect(listMessageReferences).toHaveBeenCalledWith('work','Archive',7,[1],expect.any(AbortSignal),expect.any(Object))
    expect(listMessages).not.toHaveBeenCalled();expect(inbox.rows.value[0]).toMatchObject({uid:1,folder:'Archive',validity:7})
    useSessionStore().lockLocal();organization.clear();await flushPromises();expect(inbox.rows.value).toEqual([]);wrapper.unmount()
  })
  it('hides a snoozed reference only in its source and releases it on the local clock',async()=>{
    vi.useFakeTimers();const wrapper=mount(Inbox);await flushPromises()
    const reference={accountID:'work',validity:7,uid:50},organization=useMailOrganization()
    organization.data.entries[messageKey(reference)]={reference,tags:[],snoozedUntil:Date.now()+10000}
    await wrapper.vm.$nextTick();expect(inbox.rows.value).toEqual([])
    await vi.advanceTimersByTimeAsync(31000);expect(inbox.rows.value).toHaveLength(1)
    wrapper.unmount()
  })

  it('preserves bounded concurrency across three accounts and cancels queued source reads on lock',async()=>{
    useMailboxStore().accounts=[{...account},{...account,id:'second'},{...account,id:'third'}]
    const pending:Array<()=>void>=[]
    vi.mocked(listMessages).mockImplementation(()=>new Promise(resolve=>pending.push(()=>resolve({messages:[{...row}],uid_validity:7,next_before:0,total:1}))))
    const wrapper=mount(Inbox);await flushPromises();expect(listMessages).toHaveBeenCalledTimes(2)
    pending.shift()!();await flushPromises();expect(listMessages).toHaveBeenCalledTimes(3)
    useSessionStore().lockLocal();await flushPromises();for(const finish of pending)finish();await flushPromises()
    expect(inbox.rows.value).toEqual([]);wrapper.unmount()
  })
  it('retains independent folder cursors and a configured limit in combined views',async()=>{
    vi.mocked(listMessages).mockImplementation(async(_id,_signal,before)=>({messages:before?[{...row,uid:49}]:[{...row}],uid_validity:7,next_before:before?49:50,total:2}))
    useMailboxStore().accounts[0]!.sync_folders=true
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Archive',delimiter:'/',selectable:true,messages:2,unseen:1,uid_next:51,uid_validity:7}],truncated:false})
    const wrapper=mount(Inbox);await flushPromises();inbox.chooseView('all');await flushPromises()
    await inbox.loadMore();expect(inbox.rows.value).toHaveLength(4)
    expect(listMessages).toHaveBeenCalledWith('work',expect.any(AbortSignal),50,7,'Archive')
    expect(listMessages).toHaveBeenCalledWith('work',expect.any(AbortSignal),50,7)
    wrapper.unmount()
  })
  it('merges several folders without confusing identical UIDs when reading or removing mail',async()=>{
    vi.mocked(listMessages).mockImplementation(async()=>({messages:[{...row}],uid_validity:7,next_before:0,total:1}))
    useMailboxStore().accounts[0]!.sync_folders=true
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Archive',delimiter:'/',selectable:true,messages:1,unseen:1,uid_next:51,uid_validity:7}],truncated:false})
    const wrapper=mount(Inbox);await flushPromises()
    inbox.chooseView('all');await flushPromises()
    expect(inbox.rows.value).toHaveLength(2)
    const archived=inbox.rows.value.find(row=>row.folder==='Archive')!
    await inbox.open(archived)
    expect(readMessage).toHaveBeenLastCalledWith({accountID:'work',uid:50,validity:7,folder:'Archive'},expect.any(AbortSignal))
    await inbox.removeMessage()
    expect(inbox.rows.value).toHaveLength(1);expect(inbox.rows.value[0]!.folder).toBeUndefined()
    wrapper.unmount()
  })
  it('uses different mapped Sent and Drafts folders without changing the inbox default',async()=>{
    const mailbox=useMailboxStore();mailbox.accounts=[{...account,sync_folders:true,folder_mapping:{sent:'Outgoing'}},{...account,id:'personal',sync_folders:true,folder_mapping:{sent:'Sent Items'}}]
    vi.mocked(listFolders).mockImplementation(async id=>({folders:[{name:id==='work'?'Outgoing':'Sent Items',delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:51,uid_validity:7}],truncated:false}))
    const wrapper=mount(Inbox);await flushPromises()
    expect(mailbox.view).toBe('inbox');expect(inbox.rows.value).toHaveLength(2)
    inbox.chooseView('sent');await flushPromises()
    expect(new Set(inbox.rows.value.map(row=>row.folder))).toEqual(new Set(['Outgoing','Sent Items']))
    inbox.chooseView('unread');await flushPromises()
    expect(listMessages).toHaveBeenLastCalledWith(expect.any(String),expect.any(AbortSignal),0,0,'INBOX',expect.objectContaining({seen:'unread'}))
    wrapper.unmount()
  })
  it('retries a 502 while opening a message without locking or replacing the selection',async()=>{
    vi.useFakeTimers();vi.mocked(readMessage).mockRejectedValueOnce(new APIError('REQUEST_FAILED','Bad gateway',502))
    const wrapper=mount(Inbox);await flushPromises();const opening=inbox.open(inbox.rows.value[0]!);await flushPromises()
    expect(inbox.readError.value).toContain('Retrying automatically');expect(useSessionStore().unlocked).toBe(true)
    await vi.advanceTimersByTimeAsync(3000);await opening
    expect(readMessage).toHaveBeenCalledTimes(2);expect(inbox.message.value?.uid).toBe(50);expect(inbox.readError.value).toBe('');wrapper.unmount()
  })
  it('cancels a pending message retry when the vault locks',async()=>{
    vi.useFakeTimers();vi.mocked(readMessage).mockRejectedValue(new APIError('REQUEST_FAILED','Bad gateway',502))
    const wrapper=mount(Inbox);await flushPromises();const opening=inbox.open(inbox.rows.value[0]!);await flushPromises()
    useSessionStore().lockLocal();await flushPromises();await opening;await vi.advanceTimersByTimeAsync(60000)
    expect(readMessage).toHaveBeenCalledTimes(1);expect(inbox.message.value).toBeNull();wrapper.unmount()
  })
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
