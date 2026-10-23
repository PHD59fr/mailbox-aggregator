import { mount,flushPromises } from '@vue/test-utils'
import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import MailSelectionActions from './MailSelectionActions.vue'
import { useSessionStore } from '../stores/session'
import { setMessageFlag,transferMessage } from '../api/reading'
vi.mock('../api/reading',()=>({setMessageFlag:vi.fn(),transferMessage:vi.fn(),trashMessage:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());const s=useSessionStore();s.authenticated=true;s.userID='owner';s.vmk=new Uint8Array(32)})
const selection=[1,2,3].map(uid=>({accountID:'own-account',folder:'Archive',uid,validity:7}))
it('stops a batch at the first unconfirmed result and reports only confirmed actions',async()=>{
 vi.mocked(setMessageFlag).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('Result unknown'))
 const wrapper=mount(MailSelectionActions,{props:{selection,folders:[]}})
 await wrapper.get('form').trigger('submit');await flushPromises()
 expect(setMessageFlag).toHaveBeenCalledTimes(2)
 expect(setMessageFlag).toHaveBeenNthCalledWith(1,selection[0],'seen',true,expect.any(AbortSignal))
 expect(wrapper.get('[role="status"]').text()).toContain('1 of 3 actions confirmed')
 expect(wrapper.get('[role="status"]').text()).toContain('Remaining messages were not processed')
 expect(wrapper.emitted('changed')).toHaveLength(1);wrapper.unmount()
})
it('refuses moving a selection containing different accounts',async()=>{
 const wrapper=mount(MailSelectionActions,{props:{selection:[selection[0]!,{...selection[1]!,accountID:'other-account'}],folders:[]}})
 await wrapper.get('select').setValue('move');await wrapper.get('form').trigger('submit');await flushPromises()
 expect(transferMessage).not.toHaveBeenCalled();expect(wrapper.text()).toContain('one account');wrapper.unmount()
})
it('cancels subsequent actions if the vault locks during a batch',async()=>{
 let finish!:()=>void
 vi.mocked(setMessageFlag).mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve}))
 const wrapper=mount(MailSelectionActions,{props:{selection,folders:[]}})
 await wrapper.get('form').trigger('submit');await flushPromises()
 useSessionStore().lockLocal();await flushPromises();finish();await flushPromises()
 expect(setMessageFlag).toHaveBeenCalledTimes(1);wrapper.unmount()
})
it('cancels subsequent actions when the selected mailbox changes',async()=>{
 let finish!:()=>void
 vi.mocked(setMessageFlag).mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve}))
 const wrapper=mount(MailSelectionActions,{props:{selection,folders:[],scope:'own-account:Archive'}})
 await wrapper.get('form').trigger('submit');await flushPromises()
 await wrapper.setProps({scope:'own-account:INBOX'});finish();await flushPromises()
 expect(setMessageFlag).toHaveBeenCalledTimes(1);wrapper.unmount()
})
