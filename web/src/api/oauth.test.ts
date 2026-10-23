import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import { api } from './client'
import { readPreference,updatePreference } from './preferences'
import { oauthAccess,type OAuthGrant } from './oauth'
import { useSessionStore } from '../stores/session'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),api:vi.fn()}))
vi.mock('./preferences',()=>({readPreference:vi.fn(),updatePreference:vi.fn()}))
let stored:{id:string;grants:Record<string,OAuthGrant>}
const reference={id:'grant',provider:'google' as const}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',csrf:'csrf',vmk:new Uint8Array(32),serverLockRevision:7})
  stored={id:'oauth-grants',grants:{grant:{provider:'google',email:'mail@example.test',access_token:'expired',refresh_token:'refresh-A',expires_at:Date.now()/1000-1}}}
  vi.mocked(readPreference).mockImplementation(async(_id,empty)=>Object.assign(empty(),stored))
  vi.mocked(updatePreference).mockImplementation(async(_id,empty,edit)=>{const next=Object.assign(empty(),structuredClone(stored));edit(next);Object.assign(stored,next);return next})
  vi.mocked(api).mockResolvedValue({provider:'google',access_token:'access-B',refresh_token:'refresh-B',expires_at:Date.now()/1000+3600})
})
it('shares a refresh for paired identities and persists rotated tokens through encrypted preferences',async()=>{
  const [first,second]=await Promise.all([oauthAccess(reference),oauthAccess(reference)])
  expect(first.oauth_token).toBe('access-B');expect(second).toEqual(first)
  expect(api).toHaveBeenCalledTimes(1);expect(updatePreference).toHaveBeenCalledTimes(1)
  expect(stored.grants.grant!.refresh_token).toBe('refresh-B')
  expect(JSON.parse(String(vi.mocked(api).mock.calls[0]![1]?.body))).toEqual({provider:'google',refresh_token:'refresh-A',lock_revision:7})
})
it('does not save or activate a refresh result delivered after locking',async()=>{
  let finish!:(value:unknown)=>void
  vi.mocked(api).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
  const refreshing=oauthAccess(reference)
  await vi.waitFor(()=>expect(api).toHaveBeenCalledTimes(1))
  useSessionStore().lockLocal()
  finish({provider:'google',access_token:'late',refresh_token:'late-refresh',expires_at:Date.now()/1000+3600})
  await expect(refreshing).rejects.toMatchObject({name:'AbortError'})
  expect(updatePreference).not.toHaveBeenCalled();expect(stored.grants.grant!.refresh_token).toBe('refresh-A')
})
it('does not overwrite a newer grant saved by another device during refresh',async()=>{
  vi.mocked(api).mockImplementationOnce(async()=>{
    stored.grants.grant={...stored.grants.grant!,access_token:'access-C',refresh_token:'refresh-C',expires_at:Date.now()/1000+3600}
    return {provider:'google',access_token:'access-B',refresh_token:'refresh-B',expires_at:Date.now()/1000+3600}
  })
  expect((await oauthAccess(reference)).oauth_token).toBe('access-C')
  expect(stored.grants.grant!.refresh_token).toBe('refresh-C')
})
