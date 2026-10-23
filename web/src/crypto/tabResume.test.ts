// @vitest-environment node
import {webcrypto} from 'node:crypto'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {saveTabResume,restoreTabResume,discardTabResume,touchTabResume} from './tabResume'
import {putTabKey,getTabKey,deleteTabKey} from './tabKeyStore'
import {base64} from './encoding'
vi.mock('./tabKeyStore',()=>({putTabKey:vi.fn(),getTabKey:vi.fn(),deleteTabKey:vi.fn()}))
const records=new Map<string,string>();const keys=new Map<string,CryptoKey>();let now:number
beforeEach(()=>{
  vi.resetAllMocks();records.clear();keys.clear();now=1000000
  vi.stubGlobal('crypto',webcrypto)
  vi.stubGlobal('sessionStorage',{getItem:(key:string)=>records.get(key)||null,setItem:(key:string,value:string)=>{records.set(key,value)},removeItem:(key:string)=>{records.delete(key)}})
  vi.spyOn(Date,'now').mockImplementation(()=>now)
  vi.mocked(putTabKey).mockImplementation(async(id,key)=>{keys.set(id,key)})
  vi.mocked(getTabKey).mockImplementation(async id=>keys.get(id))
  vi.mocked(deleteTabKey).mockImplementation(async id=>{keys.delete(id)})
})
afterEach(()=>{discardTabResume();vi.restoreAllMocks();vi.unstubAllGlobals()})
const save=(key=new Uint8Array(32).fill(7),active=()=>true)=>saveTabResume(key,'user','csrf',3,now,15,active)
describe('encrypted tab vault resumption',()=>{
  it('restores the same vault after reload without storing its raw key or an extractable wrapping key',async()=>{
    const vmk=new Uint8Array(32).fill(7);expect(await save(vmk)).toBe(true)
    const stored=records.get('mailbox-tab-resume-v1')!
    expect(stored).not.toContain(base64(vmk));expect(stored).not.toContain('password')
    const key=vi.mocked(putTabKey).mock.calls[0]![1];expect(key.extractable).toBe(false)
    await expect(crypto.subtle.exportKey('raw',key)).rejects.toThrow()
    const restored=await restoreTabResume('user','csrf',3)
    expect(restored?.vmk).toEqual(vmk);expect(restored?.lastActivity).toBe(now)
  })
  it.each([['another user','csrf',3],['user','another session',3],['user','csrf',4]] as const)('rejects a different user/session/lock revision',async(user,csrf,revision)=>{
    await save();expect(await restoreTabResume(user,csrf,revision)).toBeNull();expect(records.size).toBe(0);expect(keys.size).toBe(0)
  })
  it('expires on inactivity, while actual activity extends the deadline',async()=>{
    await save();now+=14*60000;touchTabResume(now,15);now+=14*60000
    expect(await restoreTabResume('user','csrf',3)).not.toBeNull()
    now+=60000;expect(await restoreTabResume('user','csrf',3)).toBeNull();expect(keys.size).toBe(0)
  })
  it('never resurrects a resume record if locked during key persistence',async()=>{
    let finish!:()=>void;let active=true
    vi.mocked(putTabKey).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
    const saving=save(new Uint8Array(32).fill(7),()=>active)
    await vi.waitFor(()=>expect(putTabKey).toHaveBeenCalled())
    active=false;discardTabResume();finish()
    expect(await saving).toBe(false);expect(records.size).toBe(0);expect(deleteTabKey).toHaveBeenCalled()
  })
  it('fails closed if encrypted storage or ciphertext is unavailable',async()=>{
    vi.mocked(putTabKey).mockRejectedValueOnce(new Error('Browser denied storage'))
    expect(await save()).toBe(false);expect(records.size).toBe(0)
    await save();const record=JSON.parse(records.get('mailbox-tab-resume-v1')!);record.ciphertext='AAAA'
    records.set('mailbox-tab-resume-v1',JSON.stringify(record))
    expect(await restoreTabResume('user','csrf',3)).toBeNull();expect(records.size).toBe(0)
  })
  it('keeps resume optional when the browser lacks a required crypto capability',async()=>{
    vi.stubGlobal('crypto',{subtle:webcrypto.subtle,getRandomValues:webcrypto.getRandomValues.bind(webcrypto)})
    expect(await save()).toBe(false);expect(records.size).toBe(0);expect(putTabKey).not.toHaveBeenCalled()
  })
})
