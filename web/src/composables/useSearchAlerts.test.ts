import {defineComponent,ref} from 'vue'
import {mount,flushPromises} from '@vue/test-utils'
import {beforeEach,it,expect,vi} from 'vitest'
import {createPinia,setActivePinia} from 'pinia'
import {useSearchAlerts} from './useSearchAlerts'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
import {listMessages} from '../api/reading'
import type {SavedSearch} from '../api/preferences'
vi.mock('../api/reading',()=>({listMessages:vi.fn()}))
beforeEach(()=>{setActivePinia(createPinia());vi.resetAllMocks();Object.defineProperty(document,'visibilityState',{value:'visible',configurable:true});useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)});useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'mail.test',imap_port:993,imap_security:'tls',imap_username:'alice',is_default:true}]})
it('baselines, deduplicates, never reads bodies and drops a late result after lock',async()=>{
 const searches=ref<SavedSearch[]>([]);let alerts:ReturnType<typeof useSearchAlerts>
 const wrapper=mount(defineComponent({setup(){alerts=useSearchAlerts(searches,()=>({}));return()=>null}}))
 const message={uid:1,subject:'Invoice',from:[],to:[],cc:[],date:'2026-10-25',seen:false,size:1}
 vi.mocked(listMessages).mockResolvedValue({messages:[message],uid_validity:7,next_before:0,total:1})
 searches.value=[{id:'search',name:'Invoices',filters:{subject:'invoice'},accountID:'work',folder:'INBOX',alert:true}];await flushPromises()
 expect(alerts!.notices.value).toEqual([])
 vi.mocked(listMessages).mockResolvedValue({messages:[message,{...message,uid:2}],uid_validity:7,next_before:0,total:2})
 await alerts!.check();expect(alerts!.notices.value).toHaveLength(1)
 await alerts!.check();expect(alerts!.notices.value).toHaveLength(1)
 let finish!:(value:Awaited<ReturnType<typeof listMessages>>)=>void
 vi.mocked(listMessages).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 const checking=alerts!.check();await flushPromises();useSessionStore().lockLocal();await flushPromises();finish({messages:[{...message,uid:3}],uid_validity:7,next_before:0,total:1});await checking
 expect(alerts!.notices.value).toEqual([]);wrapper.unmount()
})
