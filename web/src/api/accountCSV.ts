
import { t } from '../i18n'
import {csvRows} from './contactFiles'
import type {Account,SMTPConfig} from './accounts'
export interface CSVAccounts{accounts:Account[];smtp:SMTPConfig[]}
export const accountKey=(a:Pick<Account,'imap_host'|'imap_port'|'imap_username'>)=>JSON.stringify([a.imap_host.toLowerCase(),a.imap_port,a.imap_username])
export const senderKey=(s:Pick<SMTPConfig,'host'|'port'|'username'|'sender_email'>)=>JSON.stringify([s.host.toLowerCase(),s.port,s.username,s.sender_email?.toLowerCase()])
const fields=['name','imap_host','imap_port','imap_security','imap_username','imap_password','smtp_host','smtp_port','smtp_security','smtp_username','smtp_password','sender_email','sender_name']
function connection(row:Record<string,string>,prefix:'imap'|'smtp'){
 const host=row[`${prefix}_host`]||'',port=Number(row[`${prefix}_port`]),security=row[`${prefix}_security`],username=row[`${prefix}_username`]||'',password=row[`${prefix}_password`]||''
 if(!host||host.length>253||!host.replace(/\.$/,'').split('.').every(label=>label.length<=63&&/^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(label))||!Number.isInteger(port)||port<1||port>65535||!['tls','starttls'].includes(security||'')||!username||username.length>512||!password||password.length>4096)throw new Error(t("Invalid {0} connection. Use a DNS host, port, tls/starttls, username and password.", {"0": prefix.toUpperCase()}))
 return {host,port,security:security as 'tls'|'starttls',username,password}
}
export function parseAccountCSV(text:string):CSVAccounts{
 if(new globalThis.TextEncoder().encode(text).length>262144)throw new Error('Choose a CSV smaller than 256 KiB.')
 const rows=csvRows(text.replace(/^\uFEFF/,'')),headers=rows.shift()?.map(h=>h.trim().toLowerCase())||[]
 if(headers.length!==new Set(headers).size||headers.some(h=>!fields.includes(h))||!fields.slice(0,6).every(h=>headers.includes(h)))throw new Error('Use the documented CSV header; unknown or duplicate columns are rejected.')
 if(!rows.length||rows.length>50)throw new Error('Import one to fifty account rows at a time.')
 const result:CSVAccounts={accounts:[],smtp:[]},seen=new Set<string>()
 for(const [index,values] of rows.entries()){
  if(values.length!==headers.length)throw new Error(t("Row {0}: the number of fields differs from the header.", {"0": index+2}))
  const row=Object.fromEntries(headers.map((h,i)=>[h,values[i]||'']))
  if(values.some(v=>Array.from(v).some(c=>c.charCodeAt(0)<32||c.charCodeAt(0)===127)))throw new Error(t("Row {0}: control characters and line breaks are not supported.", {"0": index+2}))
  if(!row.name?.trim()||row.name.length>128)throw new Error(t("Row {0}: enter a name of at most 128 characters.", {"0": index+2}))
  const imap=connection(row,'imap'),account:Account={id:globalThis.crypto.randomUUID(),name:row.name.trim(),imap_host:imap.host,imap_port:imap.port,imap_security:imap.security,imap_username:imap.username,password:imap.password,is_default:false}
  const key=accountKey(account);if(seen.has(key))throw new Error(t("Row {0}: duplicate receiving connection.", {"0": index+2}));seen.add(key)
  if(fields.slice(6).some(k=>row[k])){
   const smtp=connection(row,'smtp'),email=row.sender_email||''
   if(!/^[^\s<>@]+@[^\s<>@]+$/.test(email)||email.length>254||(row.sender_name||'').length>128)throw new Error(t("Row {0}: enter a valid sender address and name.", {"0": index+2}))
   const sender:SMTPConfig={...smtp,id:globalThis.crypto.randomUUID(),name:account.name,sender_email:email,sender_name:row.sender_name||'',is_default:false}
   const same=result.smtp.find(s=>senderKey(s)===senderKey(sender));if(same&&same.password!==sender.password)throw new Error(t("Row {0}: conflicting passwords for the same sender.", {"0": index+2}))
   account.smtp_config_id=same?.id||sender.id;if(!same)result.smtp.push(sender)
  }
  result.accounts.push(account)
 }
 return result
}
export function mergeCSV(records:{accounts:Record<string,Account>;smtp:Record<string,SMTPConfig>},parsed:CSVAccounts,limit:number){
 const accounts={...records.accounts},smtp={...records.smtp};let added=0,duplicates=0
 for(const entry of parsed.accounts){
  if(Object.values(accounts).some(a=>accountKey(a)===accountKey(entry))){duplicates++;continue}
  const account={...entry,is_default:Object.keys(accounts).length===0};const incoming=parsed.smtp.find(s=>s.id===account.smtp_config_id)
  if(incoming){const existing=Object.values(smtp).find(s=>senderKey(s)===senderKey(incoming));if(existing)account.smtp_config_id=existing.id;else{smtp[incoming.id]={...incoming,is_default:Object.keys(smtp).length===0}}}
  accounts[account.id]=account;added++
 }
 if(Object.keys(accounts).length>limit||Object.keys(smtp).length>limit)throw new Error(t("The import exceeds the limit of {0} identities per collection. No accounts were saved.", {"0": limit}))
 return {accounts,smtp,added,duplicates}
}
