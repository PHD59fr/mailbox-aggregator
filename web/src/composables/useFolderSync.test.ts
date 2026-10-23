import {defineComponent} from 'vue'
import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {useFolderSync} from './useFolderSync'
import {listFolders} from '../api/reading'
import {APIError} from '../api/client'
import {useMailboxStore} from '../stores/mailboxes'
import {useSessionStore} from '../stores/session'

vi.mock('../api/reading',()=>({listFolders:vi.fn()}))
const account={id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls' as const,imap_username:'me',password:'secret',is_default:true,sync_folders:true}
const page={folders:[{name:'Sent',delimiter:'/',selectable:true,messages:2,unseen:1,uid_next:3,uid_validity:7}],truncated:false}
let sync:ReturnType<typeof useFolderSync>
const Folders=defineComponent({setup(){sync=useFolderSync();return()=>null}})
beforeEach(()=>{
  vi.resetAllMocks();vi.useFakeTimers();setActivePinia(createPinia())
  Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true})
  useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)})
  useMailboxStore().accounts=[{...account}];useMailboxStore().setInterval(30)
  vi.mocked(listFolders).mockResolvedValue(page)
})
afterEach(()=>{vi.useRealTimers();localStorage.removeItem('mailbox-refresh-seconds')})
describe('optional folder synchronization',()=>{
  it('uses the configured interval, pauses hidden and clears private names on lock',async()=>{
    const wrapper=mount(Folders);await flushPromises();expect(sync.folders.value.work).toEqual(page)
    await vi.advanceTimersByTimeAsync(30000);expect(listFolders).toHaveBeenCalledTimes(2)
    const signal=vi.mocked(listFolders).mock.calls[1]![1]
    Object.defineProperty(document,'visibilityState',{value:'hidden',configurable:true});document.dispatchEvent(new Event('visibilitychange'));await flushPromises()
    expect(signal.aborted).toBe(true);await vi.advanceTimersByTimeAsync(60000);expect(listFolders).toHaveBeenCalledTimes(2)
    Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true});document.dispatchEvent(new Event('visibilitychange'));await flushPromises()
    expect(listFolders).toHaveBeenCalledTimes(3)
    useSessionStore().lockLocal();await flushPromises();expect(sync.folders.value).toEqual({});wrapper.unmount()
  })
  it('retries DNS failures but does not retry a rejected password',async()=>{
    vi.mocked(listFolders).mockRejectedValueOnce(new APIError('MAIL_SERVER_UNAVAILABLE','DNS error',503))
    const wrapper=mount(Folders);await flushPromises();await vi.advanceTimersByTimeAsync(3000)
    expect(listFolders).toHaveBeenCalledTimes(2);expect(sync.folderErrors.value).toEqual({})
    vi.mocked(listFolders).mockRejectedValue(new APIError('IMAP_AUTH_FAILED','Wrong password',422))
    await vi.advanceTimersByTimeAsync(30000);const calls=vi.mocked(listFolders).mock.calls.length
    await vi.advanceTimersByTimeAsync(120000);expect(listFolders).toHaveBeenCalledTimes(calls);wrapper.unmount()
  })
  it('never lists folders for accounts without opt-in and removes names when disabled',async()=>{
    useMailboxStore().accounts[0]!.sync_folders=false
    const wrapper=mount(Folders);await flushPromises();expect(listFolders).not.toHaveBeenCalled()
    useMailboxStore().accounts[0]!.sync_folders=true;await flushPromises();expect(sync.folders.value.work).toEqual(page)
    useMailboxStore().accounts[0]!.sync_folders=false;await flushPromises();expect(sync.folders.value).toEqual({})
    await vi.advanceTimersByTimeAsync(60000);expect(listFolders).toHaveBeenCalledTimes(1);wrapper.unmount()
  })
})
