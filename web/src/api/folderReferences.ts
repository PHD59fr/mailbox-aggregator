import {api} from './client'
import {useSessionStore} from '../stores/session'

interface FolderCache {epoch:number;csrf:string;user:string;ids:Map<string,string>}
const caches=new WeakMap<object,FolderCache>()
const subscribed=new WeakSet<object>()
function cache(){
  const session=useSessionStore();let value=caches.get(session)
  if(!subscribed.has(session)){
    subscribed.add(session)
    session.$subscribe((_mutation,state)=>{const current=caches.get(session);if(!state.vmk||current&&(current.epoch!==state.vaultEpoch||current.user!==state.userID||current.csrf!==state.csrf))caches.delete(session)},{detached:true,flush:'sync'})
  }
  if(!value||value.epoch!==session.vaultEpoch||value.csrf!==session.csrf||value.user!==session.userID){value={epoch:session.vaultEpoch,csrf:session.csrf,user:session.userID,ids:new Map()};caches.set(session,value)}
  return value
}
const key=(account:string,name:string)=>JSON.stringify([account,name])
export function rememberFolderReferences(account:string,folders:{id?:string;name:string}[]){
  const value=cache()
  for(const folder of folders)if(folder.id)value.ids.set(key(account,folder.name),folder.id)
}
export function cachedFolderReference(account:string,name:string){return cache().ids.get(key(account,name))}
export async function folderReference(account:string,name:string,signal:AbortSignal){
  const session=useSessionStore(),value=cache(),vmk=session.vmk
  const check=()=>{if(signal.aborted||!session.authenticated||!vmk||session.vmk!==vmk||cache()!==value)throw new DOMException('Reading was cancelled.','AbortError')}
  check()
  const existing=value.ids.get(key(account,name));if(existing)return existing
  // Plain names travel only in the authenticated POST body, never in a URL.
  const result=await api<{id:string}>(`/mailboxes/${encodeURIComponent(account)}/folder-references`,{method:'POST',signal,cache:'no-store',body:JSON.stringify({name})})
  check()
  if(typeof result.id!=='string'||!result.id||result.id.length>1500)throw new Error('Reload the application to use private folder references.')
  value.ids.set(key(account,name),result.id);return result.id
}
