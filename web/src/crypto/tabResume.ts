import {base64,fromBase64,utf8} from './encoding'
import {putTabKey,getTabKey,deleteTabKey} from './tabKeyStore'

const storageKey='mailbox-tab-resume-v1'
let generation=0
interface ResumeRecord {version:1; id:string; userID:string; csrf:string; revision:number; nonce:string; ciphertext:string; lastActivity:number; idleMinutes:number}
const aad=(record:Pick<ResumeRecord,'id'|'userID'|'csrf'|'revision'>)=>utf8(JSON.stringify(['mailbox-tab-resume-v1',record.id,record.userID,record.csrf,record.revision])) as BufferSource
function read():ResumeRecord|null {
  try {
    const raw=sessionStorage.getItem(storageKey)
    if(!raw || raw.length>4096) return null
    const record=JSON.parse(raw) as ResumeRecord
    if(record.version!==1 || typeof record.id!=='string' || record.id.length>100 || typeof record.userID!=='string' || typeof record.csrf!=='string' || typeof record.nonce!=='string' || typeof record.ciphertext!=='string') return null
    return record
  } catch {return null}
}
export function discardTabResume() {
  generation++;const record=read()
  try {sessionStorage.removeItem(storageKey)} catch { /* Browser storage may be disabled. */ }
  if(record) void deleteTabKey(record.id).catch(()=>{})
}
export function touchTabResume(lastActivity:number,idleMinutes:number) {
  const record=read();if(!record)return
  try {sessionStorage.setItem(storageKey,JSON.stringify({...record,lastActivity,idleMinutes}))} catch {discardTabResume()}
}
export async function saveTabResume(vmk:Uint8Array,userID:string,csrf:string,revision:number,lastActivity:number,idleMinutes:number,active:()=>boolean) {
  discardTabResume();const stamp=generation
  if(!userID || !csrf || vmk.byteLength!==32) return false
  let id:string|undefined
  try {
    id=crypto.randomUUID()
    const key=await crypto.subtle.generateKey({name:'AES-GCM',length:256},false,['encrypt','decrypt'])
    const record:ResumeRecord={version:1,id,userID,csrf,revision,lastActivity,idleMinutes,nonce:'',ciphertext:''}
    const nonce=crypto.getRandomValues(new Uint8Array(12))
    record.nonce=base64(nonce)
    record.ciphertext=base64(new Uint8Array(await crypto.subtle.encrypt({name:'AES-GCM',iv:nonce,additionalData:aad(record)},key,vmk as BufferSource)))
    if(stamp!==generation || !active())return false
    await putTabKey(id,key)
    if(stamp!==generation || !active()) {await deleteTabKey(id);return false}
    sessionStorage.setItem(storageKey,JSON.stringify(record))
    return true
  } catch {if(id)void deleteTabKey(id).catch(()=>{});return false}
}
export async function restoreTabResume(userID:string,csrf:string,revision:number) {
  const record=read();const stamp=generation
  if(!record)return null
  const age=Date.now()-record.lastActivity
  if(record.userID!==userID || record.csrf!==csrf || record.revision!==revision || !Number.isFinite(age) || age<0 || ![1,5,10,15,30,60].includes(record.idleMinutes) || age>=record.idleMinutes*60000) {discardTabResume();return null}
  try {
    const key=await getTabKey(record.id)
    if(!key || key.extractable) {discardTabResume();return null}
    const raw=await crypto.subtle.decrypt({name:'AES-GCM',iv:fromBase64(record.nonce) as BufferSource,additionalData:aad(record)},key,fromBase64(record.ciphertext) as BufferSource)
    if(raw.byteLength!==32 || stamp!==generation) {new Uint8Array(raw).fill(0);return null}
    return {vmk:new Uint8Array(raw),lastActivity:record.lastActivity,idleMinutes:record.idleMinutes}
  } catch {discardTabResume();return null}
}
export function broadcastVaultLock(userID:string) {
  if(typeof window==='undefined' || typeof window.BroadcastChannel!=='function' || !userID)return
  const channel=new window.BroadcastChannel('mailbox-vault-lock-v1');channel.postMessage({userID});channel.close()
}
