import {readPreference,updatePreference,importContacts} from './preferences'
import {useSessionStore} from '../stores/session'
import type {Address} from './reading'
export interface RecipientHistory {id:'recipient-history';remember:boolean;collect:boolean;recipients:(Address&{usedAt:number})[]}
export const emptyRecipients=():RecipientHistory=>({id:'recipient-history',remember:false,collect:false,recipients:[]})
export function cleanRecipients(record:RecipientHistory){const seen=new Set<string>();return {...record,recipients:(record.recipients||[]).filter(r=>{const email=r.email?.toLowerCase();if(!email||seen.has(email)||r.usedAt<Date.now()-30*86400000||!/^[^\s<>@]+@[^\s<>@]+$/.test(email)||email.length>254)return false;seen.add(email);return true}).sort((a,b)=>b.usedAt-a.usedAt).slice(0,100)}}
export async function recipientSuggestions(){return cleanRecipients(await readPreference<RecipientHistory>('recipient-history',emptyRecipients)).recipients}
// Call only after confirmed SMTP acceptance. Bcc is deliberately excluded.
export async function rememberAcceptedRecipients(addresses:Address[]){
 const session=useSessionStore(),vmk=session.vmk,user=session.userID
 if(!vmk||!session.unlocked)return
 const current=()=>session.vmk===vmk&&session.userID===user&&session.unlocked
 const settings=await readPreference<RecipientHistory>('recipient-history',emptyRecipients);if(!current())return
 const unique=[...new Map(addresses.map(a=>[a.email.toLowerCase(),{email:a.email,name:a.name?.slice(0,128)}])).values()]
 if(settings.remember)await updatePreference<RecipientHistory>('recipient-history',emptyRecipients,record=>{
  if(!record.remember)return
  record.recipients=cleanRecipients({...record,recipients:[...unique.map(a=>({...a,usedAt:Date.now()})),...record.recipients]}).recipients
 })
 if(settings.collect&&current())await importContacts(unique.map(a=>({name:a.name||a.email.slice(0,128),email:a.email,groups:[]})))
}
