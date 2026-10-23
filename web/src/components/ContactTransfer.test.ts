import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import ContactTransfer from './ContactTransfer.vue'
import {importContacts} from '../api/preferences'
import {useSessionStore} from '../stores/session'
vi.mock('../api/preferences',()=>({importContacts:vi.fn()}))
const book={contacts:[{id:'saved',name:'Saved name',email:'saved@example.test'}],groups:[]}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32),loading:false})
  Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:vi.fn(function(this:HTMLDialogElement){this.open=true})})
  Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:vi.fn(function(this:HTMLDialogElement){this.open=false})})
})
afterEach(()=>{useSessionStore().dismissNotice();vi.restoreAllMocks()})
async function choose(wrapper:ReturnType<typeof mount>,file:File){
  const input=wrapper.get('input[type="file"]');Object.defineProperty(input.element,'files',{configurable:true,value:[file]});await input.trigger('change');await vi.waitFor(()=>expect(wrapper.text()).not.toContain('Reading contact file…'));await flushPromises()
}
it('previews new and duplicate contacts before a single explicit merge',async()=>{
  vi.mocked(importContacts).mockResolvedValue({...book,added:1,duplicates:1})
  const wrapper=mount(ContactTransfer,{props:{book},global:{stubs:{teleport:true}}})
  await wrapper.findAll('button')[0]!.trigger('click')
  await choose(wrapper,new File(['Name,Email\r\nNew,new@example.test\r\nAlready here,saved@example.test\r\nBad,invalid'], 'contacts.csv'))
  expect(wrapper.text()).toContain('1 new contacts, 1 existing or repeated addresses, 1 skipped')
  expect(importContacts).not.toHaveBeenCalled()
  await wrapper.findAll('button').find(button=>button.text()==='Import and merge')!.trigger('click');await flushPromises()
  expect(importContacts).toHaveBeenCalledWith([{name:'New',email:'new@example.test',groups:[]},{name:'Already here',email:'saved@example.test',groups:[]}])
  expect(wrapper.emitted('imported')).toHaveLength(1);expect(wrapper.find('dialog').exists()).toBe(false);wrapper.unmount()
})
it('rejects malformed files without mutating the vault and clears previews on lock',async()=>{
  const wrapper=mount(ContactTransfer,{props:{book},global:{stubs:{teleport:true}}})
  await wrapper.findAll('button')[0]!.trigger('click');await choose(wrapper,new File(['Name,Phone\nA,123'],'bad.csv'))
  expect(wrapper.get('[role="alert"]').text()).toContain('No email column');expect(importContacts).not.toHaveBeenCalled()
  await choose(wrapper,new File(['Name,Email\nPrivate,private@example.test'],'good.csv'))
  expect(wrapper.text()).toContain('private@example.test')
  useSessionStore().lockLocal(false);await flushPromises();expect(wrapper.find('dialog').exists()).toBe(false);expect(wrapper.text()).not.toContain('private@example.test');wrapper.unmount()
})
it('exports the chosen compatible format as a local download',async()=>{
  const create=vi.fn().mockReturnValue('blob:test'),revoke=vi.fn(),click=vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(()=>{})
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:create});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:revoke})
  const wrapper=mount(ContactTransfer,{props:{book},global:{stubs:{teleport:true}}})
  await wrapper.findAll('button')[1]!.trigger('click');await wrapper.get('select').setValue('vcard')
  await wrapper.findAll('button').find(button=>button.text()==='Download contacts')!.trigger('click')
  expect(create).toHaveBeenCalledTimes(1);expect((click.mock.contexts[0] as HTMLAnchorElement).download).toBe('contacts.vcf');expect(importContacts).not.toHaveBeenCalled();wrapper.unmount()
})
