import {webcrypto} from 'node:crypto'
import {createPinia,setActivePinia} from 'pinia'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {encryptFile,decryptFile,generateVMK,type EncryptedFile} from '../crypto/vault'
import {useSessionStore} from './session'
import {useThemeStore,initializeTheme} from './theme'

const ids={a:'11111111-1111-4111-8111-111111111111',b:'22222222-2222-4222-8222-222222222222'}
let keys:Record<string,Uint8Array>,files:Record<string,EncryptedFile>,failSave:boolean
let readGate:Promise<void>|undefined,writeGate:Promise<void>|undefined,writing:boolean
function signIn(id:string){const session=useSessionStore();session.accept({user_id:id,csrf_token:`csrf-${id}`,state:'AUTHENTICATED_LOCKED'});session.vmk=keys[id]!.slice()}
async function records(id:string){return decryptFile<Record<string,{id:string;theme?:string;domains?:string[]}>>(files[id]!,keys[id]!,id,'preferences')}
beforeEach(async()=>{
  vi.stubGlobal('crypto',webcrypto);setActivePinia(createPinia())
  keys={[ids.a]:generateVMK(),[ids.b]:generateVMK()};files={};failSave=false;readGate=undefined;writeGate=undefined;writing=false
  for(const id of Object.values(ids))files[id]=await encryptFile({'mail-reader':{id:'mail-reader',block_external:true,domains:['example.test'],addresses:[]}},keys[id]!,id,'preferences')
  vi.stubGlobal('fetch',vi.fn(async(url,init)=>{
    if(url!=='/api/v1/vault/files/preferences')throw new Error(`Unexpected request ${url}`)
    const id=useSessionStore().userID
    if(init?.method==='PUT'){
      writing=true;const data=JSON.parse(String(init.body));expect(new Headers(init.headers).get('If-Match')).toBe(`"${id}"`)
      if(writeGate)await writeGate
      if(failSave)return Response.json({error:{code:'REVISION_CONFLICT',message:'Preferences changed. Try again.'}},{status:412})
      files[id]=data;return new Response(null,{status:204})
    }
    const data=files[id];if(readGate)await readGate
    return Response.json(data,{headers:{ETag:`"${id}"`}})
  }))
  signIn(ids.a)
})
afterEach(()=>{useThemeStore().clear();localStorage.removeItem('mailbox-theme');vi.unstubAllGlobals()})

describe('encrypted profile appearance',()=>{
  it('stores only fixed accent/text choices encrypted and clears them on lock',async()=>{
    const theme=useThemeStore();await theme.load();await theme.setDetails('purple','large')
    expect(document.documentElement.dataset.accent).toBe('purple');expect(document.documentElement.dataset.textScale).toBe('large')
    expect(JSON.stringify(files[ids.a])).not.toContain('purple')
    await theme.setDetails('url(https://attacker.test)','large');expect(theme.accent).toBe('purple')
    useSessionStore().lockLocal();theme.clear();expect(document.documentElement.dataset.accent).toBe('theme');expect(document.documentElement.dataset.textScale).toBe('normal')
  })
  it('saves the theme encrypted in the profile while preserving other preferences',async()=>{
    localStorage.setItem('mailbox-theme','cinema');initializeTheme()
    const theme=useThemeStore();await theme.load()
    expect(theme.selection).toBe('slate');expect(localStorage.getItem('mailbox-theme')).toBeNull()
    await theme.setTheme('groove')
    expect((await records(ids.a)).appearance).toEqual({id:'appearance',theme:'groove'})
    expect((await records(ids.a))['mail-reader']?.domains).toEqual(['example.test'])
    expect(JSON.stringify(files[ids.a])).not.toContain('groove');expect(localStorage.getItem('mailbox-theme')).toBeNull()
  })
  it('keeps two account choices separate and restores them on a fresh client',async()=>{
    const theme=useThemeStore();await theme.load();await theme.setTheme('groove')
    signIn(ids.b);theme.clear();await theme.load();expect(theme.selection).toBe('slate')
    await theme.setTheme('cinema')
    setActivePinia(createPinia());signIn(ids.a);await useThemeStore().load()
    expect(useThemeStore().selection).toBe('groove');expect(document.documentElement.dataset.theme).toBe('groove')
    expect((await records(ids.b)).appearance?.theme).toBe('cinema')
  })
  it('rolls back a failed save without changing the saved profile',async()=>{
    const theme=useThemeStore();await theme.load();await theme.setTheme('paper');failSave=true
    await theme.setTheme('cinema')
    expect(theme.selection).toBe('paper');expect(theme.error).toContain('Preferences changed')
    expect((await records(ids.a)).appearance?.theme).toBe('paper')
  })
  it('ignores a load that completes after locking the vault',async()=>{
    let release!:()=>void;readGate=new Promise(resolve=>{release=resolve})
    const theme=useThemeStore();const loading=theme.load()
    useSessionStore().lockLocal();theme.clear();release();await loading
    expect(theme.loaded).toBe(false);expect(theme.error).toBe('');expect(document.documentElement.dataset.theme).toBe('slate')
  })
  it.each([false,true])('does not apply or roll back another user’s theme after a delayed save (failure=%s)',async failure=>{
    const theme=useThemeStore();await theme.load()
    let release!:()=>void;writeGate=new Promise(resolve=>{release=resolve})
    const saving=theme.setTheme('cinema');await vi.waitFor(()=>expect(writing).toBe(true))
    signIn(ids.b);theme.clear();await theme.load();expect(theme.selection).toBe('slate')
    failSave=failure;release();await saving
    expect(theme.ownerID).toBe(ids.b);expect(theme.selection).toBe('slate');expect(theme.error).toBe('')
    expect((await records(ids.b)).appearance).toBeUndefined()
  })
  it('follows the device appearance only when this profile selects System',async()=>{
    const media={matches:false,addEventListener:vi.fn()};vi.stubGlobal('matchMedia',()=>media)
    initializeTheme();const theme=useThemeStore();await theme.load();await theme.setTheme('system')
    expect(document.documentElement.dataset.theme).toBe('paper')
    media.matches=true;media.addEventListener.mock.calls[0]![1]();expect(document.documentElement.dataset.theme).toBe('slate')
    await theme.setTheme('groove');media.matches=false;media.addEventListener.mock.calls[0]![1]();expect(document.documentElement.dataset.theme).toBe('groove')
  })
})
