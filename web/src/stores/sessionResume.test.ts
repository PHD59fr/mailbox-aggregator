import {createPinia,setActivePinia} from 'pinia'
import {flushPromises} from '@vue/test-utils'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import {useSessionStore} from './session'
import {api} from '../api/client'
import {restoreTabResume,saveTabResume,discardTabResume} from '../crypto/tabResume'
vi.mock('../crypto/tabResume',()=>({restoreTabResume:vi.fn(),saveTabResume:vi.fn(),discardTabResume:vi.fn(),touchTabResume:vi.fn(),broadcastVaultLock:vi.fn()}))
vi.mock('../api/client',async original=>({...await original<typeof import('../api/client')>(),api:vi.fn()}))
const view={user_id:'user',csrf_token:'csrf',state:'AUTHENTICATED_LOCKED' as const,lock_revision:3}
beforeEach(()=>{vi.resetAllMocks();localStorage.clear();sessionStorage.clear();setActivePinia(createPinia());vi.mocked(api).mockResolvedValue(view);vi.mocked(saveTabResume).mockResolvedValue(true)})
describe('verified session resumption',()=>{
  it('restores a saved vault across reload without treating credential lease expiry as a manual lock',async()=>{
    const key=new Uint8Array(32).fill(7);const lastActivity=Date.now()-40000
    vi.mocked(restoreTabResume).mockResolvedValue({vmk:key,lastActivity,idleMinutes:5})
    const session=useSessionStore();await session.restore()
    expect(session.unlocked).toBe(true);expect(session.vmk).toBe(key);expect(session.lastActivity).toBe(lastActivity)
    expect(session.idleMinutes).toBe(5);expect(session.resumeAvailable).toBe(true);expect(api).toHaveBeenCalledTimes(2)
    expect(restoreTabResume).toHaveBeenCalledWith('user','csrf',3);session.lockLocal(false)
  })
  it('does not unlock if there is no valid encrypted resume record',async()=>{
    vi.mocked(restoreTabResume).mockResolvedValue(null);const session=useSessionStore();await session.restore()
    expect(session.authenticated).toBe(true);expect(session.unlocked).toBe(false)
  })
  it('discards a late key if manually locked while decrypting a resume record',async()=>{
    let finish!: (value:Awaited<ReturnType<typeof restoreTabResume>>)=>void
    vi.mocked(restoreTabResume).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
    const session=useSessionStore();const restoring=session.restore();await flushPromises()
    session.lockLocal(false);const key=new Uint8Array(32).fill(7);finish({vmk:key,lastActivity:Date.now(),idleMinutes:15});await restoring
    expect(session.unlocked).toBe(false);expect(key.every(byte=>byte===0)).toBe(true)
  })
  it('rejects a server-side lock occurring during resumption',async()=>{
    const key=new Uint8Array(32).fill(7);vi.mocked(restoreTabResume).mockResolvedValue({vmk:key,lastActivity:Date.now(),idleMinutes:15})
    vi.mocked(api).mockResolvedValueOnce(view).mockResolvedValueOnce({...view,lock_revision:4})
    const session=useSessionStore();await session.restore();expect(session.unlocked).toBe(false);expect(key.every(byte=>byte===0)).toBe(true)
  })
  it('clears the key and resume immediately on logout, without waiting for the server',async()=>{
    let finish!:(value:unknown)=>void;vi.mocked(api).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
    const session=useSessionStore();const key=new Uint8Array(32).fill(7);session.$patch({authenticated:true,userID:'user',csrf:'csrf',vmk:key})
    const loggingOut=session.logout();expect(session.unlocked).toBe(false);expect(key.every(byte=>byte===0)).toBe(true);expect(discardTabResume).toHaveBeenCalled()
    finish(undefined);await loggingOut;expect(session.authenticated).toBe(false)
  })
})
