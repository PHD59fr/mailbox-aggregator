import assert from 'node:assert/strict'
import {createHmac} from 'node:crypto'
import { createPinia, setActivePinia } from 'pinia'
import { registerWithOpaque, loginWithOpaque,reauthenticateWithOpaque,MFARequiredError,type AuthSession } from '../web/src/crypto/auth'
import { changePassword } from '../web/src/crypto/credentials'
import { useSessionStore } from '../web/src/stores/session'
import { api, apiResponse, resourceRevision } from '../web/src/api/client'
import { encryptFile, decryptFile, type EncryptedFile, type VaultMetadata } from '../web/src/crypto/vault'

function enrollmentCode(encoded:string){
 const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';let bits=0,value=0;const bytes:number[]=[]
 for(const char of encoded){const part=alphabet.indexOf(char);assert.ok(part>=0);value=(value<<5)|part;bits+=5;if(bits>=8){bits-=8;bytes.push((value>>bits)&255)}}
 const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));const digest=createHmac('sha1',Buffer.from(bytes)).update(counter).digest(),offset=digest[19]!&15
 return String((digest.readUInt32BE(offset)&0x7fffffff)%1000000).padStart(6,'0')
}
async function signInWithFactor(login:string,password:string,recovery:string,anonymous:()=>Promise<void>){
 let challenge:MFARequiredError|undefined
 try{await loginWithOpaque(login,password)}catch(error){if(error instanceof MFARequiredError)challenge=error;else throw error}
 assert.ok(challenge,'MFA enrollment did not protect login');await anonymous()
 return api<AuthSession>('/auth/mfa/verify',{method:'POST',body:JSON.stringify({flow_id:challenge.flowID,code:recovery})})
}

// Runs the actual frontend cryptography against an isolated Go process. The
// harness supplies a cookie jar; no mock KDF, OPAQUE or encrypted envelopes.
export async function exercise(baseURL: string) {
  const nativeFetch = globalThis.fetch
  let cookie = ''
  globalThis.fetch = async (input, init) => {
    const headers = new Headers(init?.headers)
    if(cookie) headers.set('cookie', cookie)
    const response = await nativeFetch(new URL(String(input),baseURL), { ...init, headers })
    const setCookie = response.headers.get('set-cookie')
    if(setCookie) cookie=setCookie.split(';')[0]!
    return response
  }
  setActivePinia(createPinia())
  const session=useSessionStore();session.resumeOnReload=false
  const login='rotation@example.test'
  const passwords=['original password long enough', 'changed password long enough', 'rotated password long enough']
  try {
    session.accept(await registerWithOpaque(login,passwords[0]!),login)
    await session.unlock(passwords[0]!)
    const originalKey=new Uint8Array(session.vmk!)
    const expected=[{id:'private-fixture',username:'do not log this username',password:'do not log this provider secret'}]
    const response=await apiResponse('/vault/files/accounts')
    await api('/vault/files/accounts',{method:'PUT',headers:{'if-match':resourceRevision(response)!},body:JSON.stringify(await encryptFile(expected,session.vmk!,session.userID,'accounts'))})
    session.accept(await reauthenticateWithOpaque(login,passwords[0]!),login)
    const enrollment=await api<{secret:string}>('/mfa/enroll',{method:'POST',body:JSON.stringify({lock_revision:session.serverLockRevision})})
    const confirmed=await api<{recovery_codes:string[]}>('/mfa/confirm',{method:'POST',body:JSON.stringify({code:enrollmentCode(enrollment.secret)})})
    assert.equal(confirmed.recovery_codes.length,10)
    for(let step=1;step<3;step++) {
      const oldCookie=cookie
      await changePassword(login,passwords[step-1]!,passwords[step]!,step===2)
      assert.equal(session.authenticated,false)
      assert.equal((await nativeFetch(`${baseURL}/api/v1/session`,{headers:{cookie:oldCookie}})).status,401)
      await assert.rejects(()=>loginWithOpaque(login,passwords[step-1]!))
      session.accept(await signInWithFactor(login,passwords[step]!,confirmed.recovery_codes[step-1]!,async()=>{
        assert.equal((await nativeFetch(`${baseURL}/api/v1/vault/metadata`,{headers:{cookie}})).status,401)
      }),login)
      await session.unlock(passwords[step]!)
      if(step===1) assert.deepEqual(new Uint8Array(session.vmk!),originalKey)
      else assert.notDeepEqual(new Uint8Array(session.vmk!),originalKey)
      const metadata=await api<VaultMetadata>('/vault/metadata')
      assert.equal(metadata.key_version,step===2?2:1)
      for(const name of ['config','accounts','identities','smtp','preferences']) {
        const encrypted=await api<EncryptedFile>(`/vault/files/${name}`)
        const data=await decryptFile(encrypted,session.vmk!,session.userID,name)
        if(name==='accounts') assert.deepEqual(data,expected)
        if(step===2) await assert.rejects(()=>decryptFile(encrypted,originalKey,session.userID,name))
      }
    }
    const finalKey=Buffer.from(session.vmk!).toString('hex')
    session.clearLocalSession()
    return {login,password:passwords[2]!,finalKey,expected,recovery:confirmed.recovery_codes[2]!}
  } finally { globalThis.fetch=nativeFetch }
}

export async function verifyRestored(baseURL: string, fixture: Awaited<ReturnType<typeof exercise>>) {
  const nativeFetch=globalThis.fetch
  let cookie=''
  globalThis.fetch=async(input,init)=>{
    const headers=new Headers(init?.headers);if(cookie)headers.set('cookie',cookie)
    const response=await nativeFetch(new URL(String(input),baseURL),{...init,headers})
    const setCookie=response.headers.get('set-cookie');if(setCookie)cookie=setCookie.split(';')[0]!
    return response
  }
  setActivePinia(createPinia());const session=useSessionStore();session.resumeOnReload=false
  try {
    session.accept(await signInWithFactor(fixture.login,fixture.password,fixture.recovery,async()=>{
      assert.equal((await nativeFetch(`${baseURL}/api/v1/vault/metadata`)).status,401)
    }),fixture.login)
    await session.unlock(fixture.password)
    assert.equal(Buffer.from(session.vmk!).toString('hex'),fixture.finalKey)
    assert.deepEqual(await decryptFile(await api<EncryptedFile>('/vault/files/accounts'),session.vmk!,session.userID,'accounts'),fixture.expected)
  } finally {session.clearLocalSession();globalThis.fetch=nativeFetch}
}
