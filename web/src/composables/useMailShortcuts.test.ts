import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import { useMailShortcuts } from './useMailShortcuts'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
beforeEach(()=>{setActivePinia(createPinia());const s=useSessionStore();s.authenticated=true;s.vmk=new Uint8Array(32);useMailboxStore().display.keyboard=true})
it('ignores typing, modifier keys, repeats and locked sessions; removes its listener on unmount',()=>{
  const next=vi.fn();const wrapper=mount(defineComponent({setup(){useMailShortcuts({next});return()=>null}}))
  document.dispatchEvent(new KeyboardEvent('keydown',{key:'j'}));expect(next).toHaveBeenCalledTimes(1)
  const input=document.createElement('input');document.body.append(input);input.dispatchEvent(new KeyboardEvent('keydown',{key:'j',bubbles:true}));input.remove()
  document.dispatchEvent(new KeyboardEvent('keydown',{key:'j',ctrlKey:true}));document.dispatchEvent(new KeyboardEvent('keydown',{key:'j',repeat:true}))
  useSessionStore().lockLocal();document.dispatchEvent(new KeyboardEvent('keydown',{key:'j'}));expect(next).toHaveBeenCalledTimes(1)
  wrapper.unmount();useSessionStore().vmk=new Uint8Array(32);document.dispatchEvent(new KeyboardEvent('keydown',{key:'j'}));expect(next).toHaveBeenCalledTimes(1)
})
