import { mount,flushPromises } from '@vue/test-utils'
import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import AddMessageContact from './AddMessageContact.vue'
import {listContactBook,saveContact} from '../api/preferences'
import {useSessionStore} from '../stores/session'
vi.mock('../api/preferences',()=>({listContactBook:vi.fn(),saveContact:vi.fn()}))
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32),loading:false})
  vi.mocked(listContactBook).mockResolvedValue({contacts:[],groups:[{id:'work',name:'Work'}]})
  vi.mocked(saveContact).mockResolvedValue([])
  Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:vi.fn(function(this:HTMLDialogElement){this.open=true})})
  Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:vi.fn(function(this:HTMLDialogElement){this.open=false})})
})
it('saves the selected sender or recipient explicitly and assigns groups',async()=>{
  const wrapper=mount(AddMessageContact,{props:{addresses:[{name:'Alex',email:'alex@example.test'},{email:'recipient@example.test'},{email:'ALEX@example.test'}]},global:{stubs:{teleport:true}}})
  expect(saveContact).not.toHaveBeenCalled();await wrapper.get('button').trigger('click');await flushPromises()
  expect(wrapper.findAll('option')).toHaveLength(2);expect(wrapper.get<HTMLInputElement>('input[maxlength]').element.value).toBe('Alex')
  await wrapper.get('select').setValue('recipient@example.test');await wrapper.get('input[maxlength]').setValue('Recipient')
  await wrapper.get('input[type="checkbox"]').setValue(true);await wrapper.get('form').trigger('submit');await flushPromises()
  expect(saveContact).toHaveBeenCalledWith({id:'',name:'Recipient',email:'recipient@example.test',group_ids:['work']})
  expect(wrapper.find('dialog').exists()).toBe(false);wrapper.unmount();useSessionStore().dismissNotice()
})
it('edits an existing contact without duplicating it or clearing its memberships',async()=>{
  vi.mocked(listContactBook).mockResolvedValue({contacts:[{id:'saved',name:'My name',email:'alex@example.test',group_ids:['work']}],groups:[{id:'work',name:'Work'}]})
  const wrapper=mount(AddMessageContact,{props:{addresses:[{name:'Header name',email:'alex@example.test'}]},global:{stubs:{teleport:true}}})
  await wrapper.get('button').trigger('click');await flushPromises();expect(wrapper.text()).toContain('already saved')
  await wrapper.get('form').trigger('submit');await flushPromises()
  expect(saveContact).toHaveBeenCalledWith({id:'saved',name:'My name',email:'alex@example.test',group_ids:['work']})
  wrapper.unmount();useSessionStore().dismissNotice()
})
it('clears the dialog and ignores a lookup arriving after locking',async()=>{
  let resolve!:(value:Awaited<ReturnType<typeof listContactBook>>)=>void
  vi.mocked(listContactBook).mockImplementation(()=>new Promise(done=>{resolve=done}))
  const wrapper=mount(AddMessageContact,{props:{addresses:[{name:'Private name',email:'private@example.test'}]},global:{stubs:{teleport:true}}})
  await wrapper.get('button').trigger('click');useSessionStore().lockLocal(false);await flushPromises()
  resolve({contacts:[],groups:[{id:'secret',name:'Secret group'}]});await flushPromises()
  expect(wrapper.find('dialog').exists()).toBe(false);expect(wrapper.text()).not.toContain('Secret group');expect(saveContact).not.toHaveBeenCalled();wrapper.unmount()
})
it('uses a labelled icon for one address and keeps the dialog open on an equivalent rerender',async()=>{
  const address={name:'Recipient',email:'recipient@example.test'}
  const wrapper=mount(AddMessageContact,{props:{addresses:[address],iconOnly:true},global:{stubs:{teleport:true}}})
  const button=wrapper.get('button')
  expect(button.text()).toBe('');expect(button.attributes('aria-label')).toBe('Add recipient@example.test to contacts')
  await button.trigger('click');await flushPromises()
  expect(wrapper.get<HTMLInputElement>('input[type="email"]').element.value).toBe(address.email)
  expect(wrapper.get('input[type="email"]').attributes('readonly')).toBeDefined()
  await wrapper.setProps({addresses:[{...address}]});await flushPromises()
  expect(wrapper.find('dialog').exists()).toBe(true)
  await wrapper.get('form').trigger('submit');await flushPromises()
  expect(saveContact).toHaveBeenCalledWith({id:'',name:address.name,email:address.email,group_ids:[]})
  wrapper.unmount();useSessionStore().dismissNotice()
})
