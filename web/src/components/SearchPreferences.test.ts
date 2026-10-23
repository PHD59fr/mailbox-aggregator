import {flushPromises,mount} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {beforeEach,expect,it,vi} from 'vitest'
import SearchPreferences from './SearchPreferences.vue'
import {listSavedSearches,readPreference,saveSearch,updatePreference} from '../api/preferences'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'

vi.mock('../api/preferences',()=>({listSavedSearches:vi.fn(),readPreference:vi.fn(),saveSearch:vi.fn(),deleteSearch:vi.fn(),updatePreference:vi.fn()}))
vi.mock('../composables/useFolderSync',async()=>{
  const {ref}=await import('vue')
  return {useFolderSync:()=>({folders:ref({work:{folders:[{name:'Invoices',selectable:true}],truncated:false}}),folderErrors:ref({})})}
})
const saved={id:'saved',name:'Invoices',accountID:'work',folder:'Archive',alert:false,filters:{subject:'Invoice'}}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia())
  useSessionStore().$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})
  useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true}]
  vi.mocked(listSavedSearches).mockResolvedValue([saved])
  vi.mocked(readPreference).mockImplementation(async(_id,empty)=>empty())
  vi.mocked(updatePreference).mockImplementation(async(_id,empty,change)=>{const record=empty();change(record);return record})
  vi.mocked(saveSearch).mockResolvedValue([{...saved,alert:true}])
})
it('saves combined folders and history preferences without changing the active search',async()=>{
  const mailbox=useMailboxStore();mailbox.search='Keep my active query'
  const wrapper=mount(SearchPreferences);await flushPromises()
  const details=wrapper.findAll('details')
  await details[0]!.findAll('input[type="checkbox"]')[1]!.setValue(true);await flushPromises()
  expect(mailbox.selectedFolders).toEqual([{accountID:'work',folder:'Invoices'}])
  await details[2]!.get('input[type="checkbox"]').setValue(true);await flushPromises()
  expect(updatePreference).toHaveBeenCalledWith('search-history',expect.any(Function),expect.any(Function))
  expect(mailbox.search).toBe('Keep my active query')
  wrapper.unmount()
})
it('updates an alert while preserving the saved folder and query',async()=>{
  const wrapper=mount(SearchPreferences);await flushPromises()
  await wrapper.findAll('details')[1]!.get('input[type="checkbox"]').setValue(true);await flushPromises()
  expect(saveSearch).toHaveBeenCalledWith({...saved,alert:true})
  wrapper.unmount()
})
it('discards late preference results after locking',async()=>{
  let resolve!:(value:Awaited<ReturnType<typeof listSavedSearches>>)=>void
  vi.mocked(listSavedSearches).mockReturnValue(new Promise(r=>{resolve=r}))
  const wrapper=mount(SearchPreferences)
  useSessionStore().lockLocal();await flushPromises()
  resolve([saved]);await flushPromises()
  expect(wrapper.text()).not.toContain('Invoices');expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
  wrapper.unmount()
})
