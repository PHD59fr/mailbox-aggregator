import {createPinia,setActivePinia} from 'pinia'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import {api} from './client'
import {oauthAccess} from './oauth'
import {googleContactPage} from './googleContacts'
import {useSessionStore} from '../stores/session'
vi.mock('./client',()=>({api:vi.fn()}))
vi.mock('./oauth',()=>({oauthAccess:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})})
describe('session-bound Google lookup',()=>{
  it('rejects after lock before a token can be sent to People API',async()=>{
    let resolve!:(value:{oauth_token:string;lifetime:number})=>void
    vi.mocked(oauthAccess).mockReturnValue(new Promise(r=>{resolve=r}))
    const pending=googleContactPage('g','',new AbortController().signal)
    useSessionStore().lockLocal();resolve({oauth_token:'secret',lifetime:60})
    await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(api).not.toHaveBeenCalled()
  })
  it('keeps paging explicit and returns only within the same session',async()=>{
    vi.mocked(oauthAccess).mockResolvedValue({oauth_token:'secret',lifetime:60})
    vi.mocked(api).mockResolvedValue({contacts:[{name:'Alice',email:'a@example.test'}],next_page:'next'})
    expect((await googleContactPage('g','page',new AbortController().signal)).contacts).toHaveLength(1)
    expect(api).toHaveBeenCalledTimes(1)
    expect(JSON.parse(vi.mocked(api).mock.calls[0]![1]!.body as string)).toMatchObject({page:'page',access_token:'secret'})
  })
})
