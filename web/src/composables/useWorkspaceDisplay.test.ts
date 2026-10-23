import {defineComponent} from 'vue'
import {flushPromises,mount} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {beforeEach,expect,it,vi} from 'vitest'
import {useWorkspaceDisplay} from './useWorkspaceDisplay'
import {readPreference} from '../api/preferences'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
import {defaultShortcuts} from '../api/workspaceDisplay'
vi.mock('../api/preferences',()=>({readPreference:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})})
const host=defineComponent({setup(){useWorkspaceDisplay();return()=>null}})
it('applies the encrypted profile to mail without mounting a settings form',async()=>{
  vi.mocked(readPreference).mockImplementation(async()=>({id:'workspace-display',...defaultShortcuts(),density:'compact',layout:'below',windowDays:30,windowLimit:100,colors:{work:'blue'},keyboard:true}))
  const wrapper=mount(host);await flushPromises()
  expect(useMailboxStore().display).toMatchObject({density:'compact',layout:'below',windowDays:30,windowLimit:100,keyboard:true,colors:{work:'blue'}})
  expect(wrapper.text()).toBe('');wrapper.unmount()
})
it('ignores saved display values that arrive after lock',async()=>{
  let resolve!:(value:{id:string;density:string})=>void
  vi.mocked(readPreference).mockReturnValue(new Promise(r=>{resolve=r}))
  const wrapper=mount(host)
  useSessionStore().lockLocal();await flushPromises();resolve({id:'workspace-display',density:'compact'});await flushPromises()
  expect(useMailboxStore().display.density).toBe('comfortable');wrapper.unmount()
})
