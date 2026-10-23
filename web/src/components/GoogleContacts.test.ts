import {flushPromises,mount} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import GoogleContacts from './GoogleContacts.vue'
import {api} from '../api/client'
import {oauthGrantList} from '../api/oauth'
import {googleContactPage} from '../api/googleContacts'
import {importContacts} from '../api/preferences'
import {useSessionStore} from '../stores/session'

vi.mock('../api/client',()=>({api:vi.fn()}))
vi.mock('../api/oauth',()=>({oauthGrantList:vi.fn()}))
vi.mock('../api/googleContacts',()=>({googleContactPage:vi.fn()}))
vi.mock('../api/preferences',()=>({importContacts:vi.fn()}))
const grant={id:'g',provider:'google' as const,email:'owner@example.test',scope:'https://www.googleapis.com/auth/contacts.readonly'}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia())
  useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)})
  vi.mocked(api).mockResolvedValue({google_contacts:true})
  vi.mocked(oauthGrantList).mockResolvedValue([grant])
  vi.mocked(googleContactPage).mockResolvedValue({contacts:[{name:'Existing',email:'EXISTING@example.test'},{name:'Alice',email:'alice@example.test'},{name:'Duplicate',email:'ALICE@example.test'},{name:'Bob',email:'bob@example.test'}],next_page:'next'})
})
describe('Google recipients without local import',()=>{
  it('loads only on request and deduplicates, excludes existing addresses and enforces the recipient budget',async()=>{
    const wrapper=mount(GoogleContacts,{props:{recipientMode:true,existing:['existing@example.test'],limit:1}});await flushPromises()
    expect(googleContactPage).not.toHaveBeenCalled()
    await wrapper.get('select').setValue('g');await wrapper.findAll('button')[0]!.trigger('click');await flushPromises()
    const boxes=wrapper.findAll('input[type="checkbox"]');expect(boxes).toHaveLength(3)
    expect(boxes[0]!.attributes('disabled')).toBeDefined()
    await boxes[1]!.setValue(true);expect(boxes[2]!.attributes('disabled')).toBeDefined()
    await wrapper.findAll('button').find(b=>b.text()==='Add selected Google recipients')!.trigger('click')
    expect(wrapper.emitted('select')).toEqual([[[{id:'google:g:alice@example.test',name:'Alice',email:'alice@example.test'}]]])
    expect(importContacts).not.toHaveBeenCalled();wrapper.unmount()
  })
  it('ignores an outstanding page after lock and never emits its addresses',async()=>{
    let resolve!:(value:Awaited<ReturnType<typeof googleContactPage>>)=>void
    vi.mocked(googleContactPage).mockReturnValue(new Promise(r=>{resolve=r}))
    const wrapper=mount(GoogleContacts,{props:{recipientMode:true}});await flushPromises()
    await wrapper.get('select').setValue('g');await wrapper.findAll('button')[0]!.trigger('click')
    useSessionStore().lockLocal();await flushPromises()
    resolve({contacts:[{name:'Private',email:'private@example.test'}],next_page:''});await flushPromises()
    expect(wrapper.text()).not.toContain('private@example.test');expect(wrapper.emitted('select')).toBeUndefined()
    expect(importContacts).not.toHaveBeenCalled();wrapper.unmount()
  })
  it('does not offer grants without separate readonly consent',async()=>{
    vi.mocked(oauthGrantList).mockResolvedValue([{...grant,scope:'openid email'}])
    const wrapper=mount(GoogleContacts,{props:{recipientMode:true}});await flushPromises()
    expect(wrapper.findAll('option')).toHaveLength(1);expect(googleContactPage).not.toHaveBeenCalled();wrapper.unmount()
  })
})
