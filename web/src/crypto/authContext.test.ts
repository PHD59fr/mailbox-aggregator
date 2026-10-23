import {afterEach,describe,expect,it,vi} from 'vitest'
import {loginWithOpaque,registerWithOpaque} from './auth'
import {HTTPS_REQUIRED_MESSAGE} from './secureContext'
afterEach(()=>vi.unstubAllGlobals())
describe('secure browser sign-in context',()=>{
  it('rejects insecure mobile/IP access before any authentication request',async()=>{
    const fetch=vi.fn();vi.stubGlobal('fetch',fetch);vi.stubGlobal('isSecureContext',false)
    await expect(loginWithOpaque('user','password')).rejects.toMatchObject({code:'HTTPS_REQUIRED',message:HTTPS_REQUIRED_MESSAGE})
    await expect(registerWithOpaque('user','password')).rejects.toMatchObject({code:'HTTPS_REQUIRED',message:HTTPS_REQUIRED_MESSAGE})
    expect(fetch).not.toHaveBeenCalled()
  })
})
