import {defineComponent} from 'vue'
import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {useVaultActivity} from './useVaultActivity'
import {useSessionStore} from '../stores/session'
import {api} from '../api/client'
vi.mock('../crypto/tabKeyStore',()=>({pruneTabKeys:vi.fn().mockResolvedValue(undefined)}))
vi.mock('../crypto/tabResume',()=>({discardTabResume:vi.fn(),touchTabResume:vi.fn(),saveTabResume:vi.fn().mockResolvedValue(false),restoreTabResume:vi.fn(),broadcastVaultLock:vi.fn()}))
vi.mock('../api/client',async original=>({...await original<typeof import('../api/client')>(),api:vi.fn().mockResolvedValue(undefined)}))
const Activity=defineComponent({setup(){useVaultActivity();return()=>null}})
beforeEach(()=>{vi.useFakeTimers();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,loading:false,userID:'user',csrf:'csrf',vmk:new Uint8Array(32).fill(7),lastActivity:Date.now(),idleMinutes:1,resumeOnReload:false});vi.mocked(api).mockClear()})
afterEach(()=>{vi.useRealTimers()})
describe('human inactivity vault lock',()=>{
  it('locks after configured inactivity even if mailbox polling continues',async()=>{
    const wrapper=mount(Activity);await flushPromises();const key=useSessionStore().vmk!
    await vi.advanceTimersByTimeAsync(60000)
    expect(useSessionStore().unlocked).toBe(false);expect(key.every(byte=>byte===0)).toBe(true)
    expect(api).toHaveBeenCalledWith('/session/credentials',{method:'DELETE'});wrapper.unmount()
  })
  it('extends the timer for real UI events but not background requests',async()=>{
    const wrapper=mount(Activity);await flushPromises();await vi.advanceTimersByTimeAsync(50000)
    window.dispatchEvent(new Event('keydown'));await vi.advanceTimersByTimeAsync(50000)
    expect(useSessionStore().unlocked).toBe(true)
    await vi.advanceTimersByTimeAsync(10000);expect(useSessionStore().unlocked).toBe(false);wrapper.unmount()
  })
  it('honors a changed timeout and cancels listeners/timers on unmount',async()=>{
    const wrapper=mount(Activity);await flushPromises();useSessionStore().setIdleMinutes(5);await flushPromises()
    await vi.advanceTimersByTimeAsync(60000);expect(useSessionStore().unlocked).toBe(true)
    wrapper.unmount();const last=useSessionStore().lastActivity;window.dispatchEvent(new Event('pointerdown'))
    expect(useSessionStore().lastActivity).toBe(last);await vi.advanceTimersByTimeAsync(300000);expect(useSessionStore().unlocked).toBe(true)
  })
})
