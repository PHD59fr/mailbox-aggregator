import { mount,flushPromises } from '@vue/test-utils'
import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import ComposeTemplates from './ComposeTemplates.vue'
import { readPreference,updatePreference } from '../api/preferences'
import { useSessionStore } from '../stores/session'
vi.mock('../api/preferences',()=>({readPreference:vi.fn(),updatePreference:vi.fn()}))
const draft={to:'to@example.test',cc:'',subject:'Hello',text:'Body',html:'<p>Body</p>'}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)})
  vi.mocked(readPreference).mockImplementation(async(_id,empty)=>empty())
  vi.mocked(updatePreference).mockImplementation(async(_id,empty,change)=>{const record=empty();change(record);return record})
})
it('stores a bounded template through the encrypted preference API and inserts it explicitly',async()=>{
  const wrapper=mount(ComposeTemplates,{props:{draft}});await flushPromises()
  await wrapper.get('input').setValue('Greeting')
  await wrapper.findAll('button').find(button=>button.text()==='Save current message as template')!.trigger('click');await flushPromises()
  expect(updatePreference).toHaveBeenCalledWith('compose-templates',expect.any(Function),expect.any(Function))
  await wrapper.findAll('button').find(button=>button.text()==='Insert into current message')!.trigger('click')
  expect(wrapper.emitted('insert')?.[0]?.[0]).toEqual({...draft,id:expect.any(String),name:'Greeting'})
  useSessionStore().lockLocal();await flushPromises();expect(wrapper.findAll('option')).toHaveLength(1)
  wrapper.unmount()
})
