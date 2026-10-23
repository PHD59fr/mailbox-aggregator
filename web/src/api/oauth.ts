
import { api,APIError } from './client'
import { readPreference,updatePreference } from './preferences'
import { useSessionStore } from '../stores/session'

export interface OAuthReference {id:string;provider:'google'|'microsoft'}
export interface OAuthGrant {provider:'google'|'microsoft';email:string;access_token:string;refresh_token:string;expires_at:number;scope?:string}
interface Grants {id:'oauth-grants';grants:Record<string,OAuthGrant>}
const empty=():Grants=>({id:'oauth-grants',grants:{}})
const pending=new WeakMap<object,Map<string,Promise<{oauth_token:string;lifetime:number}>>>()
export function validOAuthReference(value:OAuthReference){return !!value&&typeof value.id==='string'&&value.id.length>0&&value.id.length<=128&&!['__proto__','constructor','prototype'].includes(value.id)&&['google','microsoft'].includes(value.provider)}
export async function storeOAuthGrant(grant:OAuthGrant,id=globalThis.crypto.randomUUID()):Promise<OAuthReference>{
  if(!['google','microsoft'].includes(grant.provider)||!grant.access_token||!grant.refresh_token||grant.refresh_token.length>16384||grant.access_token.length>16384||!Number.isFinite(grant.expires_at))throw new Error('The provider did not return complete offline authorization. Reconnect and grant consent again.')
  await updatePreference<Grants>('oauth-grants',empty,record=>{if(!record.grants)record.grants={};if(!record.grants[id]&&Object.keys(record.grants).length>=100)throw new Error('Remove unused OAuth grants before adding another.');record.grants[id]={...grant}})
  return {id,provider:grant.provider}
}
export async function removeOAuthGrant(id:string,revoke=false){
  const record=await readPreference<Grants>('oauth-grants',empty),grant=record.grants[id]
  if(revoke&&grant?.provider==='google')await api('/oauth/revoke',{method:'POST',body:JSON.stringify({provider:grant.provider,refresh_token:grant.refresh_token,lock_revision:useSessionStore().serverLockRevision})})
  await updatePreference<Grants>('oauth-grants',empty,current=>{delete current.grants[id]})
}
export async function oauthGrantList(){const record=await readPreference<Grants>('oauth-grants',empty);return Object.entries(record.grants||{}).map(([id,value])=>({id,provider:value.provider,email:value.email,scope:value.scope}))}
export async function oauthAccess(reference:OAuthReference,signal?:AbortSignal){
  if(!validOAuthReference(reference))throw new Error('Reconnect this OAuth account in Settings.')
  const session=useSessionStore(),{vmk,userID,vaultEpoch,csrf}=session
  const check=()=>{if(signal?.aborted||!vmk||session.vmk!==vmk||session.userID!==userID||session.vaultEpoch!==vaultEpoch||session.csrf!==csrf)throw new DOMException('OAuth activation was cancelled.','AbortError')}
  check()
  let queue=pending.get(session);if(!queue){queue=new Map();pending.set(session,queue)}
  const key=`${userID}:${csrf}:${vaultEpoch}:${reference.id}`
  let running=queue.get(key)
  if(!running){
    const refresh=async()=>{
      check()
      let record=await readPreference<Grants>('oauth-grants',empty);check()
      let grant=record.grants[reference.id]
      if(!grant||grant.provider!==reference.provider)throw new Error('This OAuth grant was removed. Reconnect the account in Settings.')
      if(!grant.access_token||grant.expires_at<=Date.now()/1000+90){
        let updated:Omit<OAuthGrant,'email'>|undefined
        for(let attempt=0;attempt<2;attempt++){
          const previous=grant.refresh_token
          try{updated=await api<Omit<OAuthGrant,'email'>>('/oauth/refresh',{method:'POST',signal,body:JSON.stringify({provider:grant.provider,refresh_token:previous,lock_revision:session.serverLockRevision})});check();break}
          catch(value){check();if(attempt===1||!(value instanceof APIError)||value.code!=='OAUTH_REFRESH_FAILED')throw value;record=await readPreference<Grants>('oauth-grants',empty);check();const newer=record.grants[reference.id];if(!newer||newer.refresh_token===previous)throw value;grant=newer}
        }
        if(!updated?.access_token)throw new Error('OAuth refresh did not return an access token.')
        const refreshToken=grant.refresh_token,provider=grant.provider,email=grant.email
        const result=await updatePreference<Grants>('oauth-grants',empty,current=>{
          const latest=current.grants[reference.id]
          if(!latest||latest.provider!==provider)throw new Error('The OAuth grant changed. Retry from Settings.')
          if(latest.refresh_token!==refreshToken){if(latest.expires_at>Date.now()/1000+30)return;throw new Error('Another device changed the OAuth grant. Retry the operation.')}
          current.grants[reference.id]={...updated!,provider,email,scope:updated!.scope||latest.scope,refresh_token:updated!.refresh_token||refreshToken}
        });check();grant=result.grants[reference.id]!
      }
      return {oauth_token:grant.access_token,lifetime:Math.max(1,Math.min(session.activationLifetimeSeconds,Math.floor(grant.expires_at-Date.now()/1000-15)))}
    }
    running=(async()=>{
      const locks=globalThis.navigator?.locks
      if(!locks)return refresh()
      let entered=false
      try{return await locks.request(`mailbox-oauth:${userID}:${reference.id}`,{...(signal?{signal}:{})},()=>{entered=true;return refresh()})}
      catch(value){if(entered||signal?.aborted)throw value;return refresh()}
    })()
    queue.set(key,running)
    void running.finally(()=>{if(queue!.get(key)===running)queue!.delete(key)}).catch(()=>{})
  }
  const result=await running;check();return result
}
