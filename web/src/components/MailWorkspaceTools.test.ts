import { mount,flushPromises } from '@vue/test-utils'
import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import MailWorkspaceTools from './MailWorkspaceTools.vue'
import { listSavedSearches,saveSearch,readPreference,updatePreference } from '../api/preferences'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import {listMessages} from '../api/reading'
vi.mock('../api/reading',()=>({listMessages:vi.fn()}))
vi.mock('../api/preferences',()=>({listSavedSearches:vi.fn(),saveSearch:vi.fn(),deleteSearch:vi.fn(),readPreference:vi.fn(),updatePreference:vi.fn()}))
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)})
  Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true})
  vi.mocked(listMessages).mockResolvedValue({messages:[],uid_validity:7,next_before:0,total:0})
  vi.mocked(listSavedSearches).mockResolvedValue([])
  vi.mocked(readPreference).mockImplementation(async(_id,empty)=>empty())
  vi.mocked(saveSearch).mockResolvedValue([])
  vi.mocked(updatePreference).mockImplementation(async(_id,empty,change)=>{const record=empty();change(record);return record})
})
it('saves query criteria and exact view scope without message content',async()=>{
  const mailbox=useMailboxStore();mailbox.filtersOpen=true;mailbox.search='invoice';mailbox.advancedSearch='NOT is:read';mailbox.view='sent';mailbox.searchScope='all'
  const wrapper=mount(MailWorkspaceTools,{props:{accountID:'',folder:'INBOX',folders:{}}});await flushPromises()
  await wrapper.get('input[placeholder="For example: unread invoices"]').setValue('Invoices')
  const button=wrapper.findAll('button').find(button=>button.text()==='Save current search')!
  await button.trigger('click');await flushPromises()
  expect(saveSearch).toHaveBeenCalledWith(expect.objectContaining({name:'Invoices',view:'sent',scope:'all',filters:expect.objectContaining({search:'invoice',expression:'NOT is:read'})}))
  expect(JSON.stringify(vi.mocked(saveSearch).mock.calls)).not.toContain('messages')
  wrapper.unmount()
})
it('keeps search actions in the filters and leaves profile settings out of mail',async()=>{
  const wrapper=mount(MailWorkspaceTools,{props:{accountID:'',folder:'INBOX',folders:{}},attachTo:document.body});await flushPromises()
  expect(wrapper.get('fieldset').isVisible()).toBe(false)
  useMailboxStore().filtersOpen=true;await flushPromises()
  expect(wrapper.get('fieldset').isVisible()).toBe(true)
  expect(wrapper.text()).toContain('Save current search')
  expect(wrapper.text()).not.toContain('Saved searches and combined folders')
  expect(wrapper.text()).not.toContain('Remember searches encrypted')
  wrapper.unmount()
})
it('continues checking saved-search alerts while the filter controls are closed',async()=>{
  vi.mocked(listSavedSearches).mockResolvedValue([{id:'alert',name:'Invoices',accountID:'work',folder:'INBOX',alert:true,filters:{subject:'Invoice'}}])
  const wrapper=mount(MailWorkspaceTools,{props:{accountID:'',folder:'INBOX',folders:{}}});await flushPromises()
  expect(wrapper.get('fieldset').isVisible()).toBe(false)
  expect(listMessages).toHaveBeenCalledWith('work',expect.any(AbortSignal),0,0,'INBOX',{subject:'Invoice'})
  wrapper.unmount()
})
it('does not restore encrypted preferences after locking while their read is pending',async()=>{
  let finish!:(value:Awaited<ReturnType<typeof listSavedSearches>>)=>void
  vi.mocked(listSavedSearches).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
  const wrapper=mount(MailWorkspaceTools,{props:{accountID:'',folder:'INBOX',folders:{}}})
  useSessionStore().lockLocal();await flushPromises()
  finish([{id:'private',name:'Private query',accountID:'',folder:'INBOX',filters:{search:'secret'}}]);await flushPromises()
  expect(wrapper.text()).not.toContain('Private query');expect(useMailboxStore().selectedFolders).toEqual([])
  wrapper.unmount()
})
