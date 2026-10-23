
import { api, apiResponse } from './client'
import { listAccounts } from './accounts'
import { useSessionStore } from '../stores/session'
import { activateCredentials } from './credentials'
import {oauthAccess} from './oauth'
import { retryRead } from './recovery'
import { cachedFolderReference, folderReference, rememberFolderReferences } from './folderReferences'

export interface Address { name?: string; email: string }
export interface MessageSummary { uid: number; subject: string; from: Address[]; to: Address[]; cc: Address[]; bcc?:Address[];date: string; seen: boolean; flagged?: boolean; answered?: boolean; draft?:boolean; size: number }
export interface MessagePage { messages: MessageSummary[]; uid_validity: number; next_before: number; total: number }
export interface Message extends MessageSummary {headers?:Record<string,string>; uid_validity: number; text: string; html: string; attachments: Attachment[]; truncated: boolean; css?: string; reply_to?: Address[]; message_id?: string;in_reply_to?:string; references?:string[] }
export interface Attachment {content_id?:string; part: string; filename: string; content_type: string; size: number }
export interface MessageRef { accountID: string; uid: number; validity: number; folder?: string; folderID?:string }
export interface MailFolder { id?:string; name: string; delimiter: string; selectable: boolean; messages: number; unseen: number; uid_next: number; uid_validity: number; error?: string; role?: string; special_use?: boolean }
export interface FolderPage { folders: MailFolder[]; truncated: boolean }
export type FolderAction = 'create' | 'rename' | 'subscribe' | 'unsubscribe' | 'delete'
export interface MessageFilters { search?: string; expression?:string; seen?: '' | 'read' | 'unread'; from?: string;to?:string;subject?:string;since?:string;until?:string;flagged?:'true' }

async function accessAccount(accountID: string, signal: AbortSignal, requireTrash = false) {
  const session = useSessionStore()
  const { userID, vmk, vaultEpoch } = session
  const check = () => {
    if (signal.aborted || !vmk || !session.authenticated || session.userID !== userID || session.vmk !== vmk || session.vaultEpoch !== vaultEpoch) throw new DOMException('Reading was cancelled.', 'AbortError')
  }
  check()
  const accounts = await listAccounts(); check()
  if (requireTrash && accounts[accountID]?.folder_mapping?.trash === null) throw new Error('Trash is set to None for this mailbox. Choose a Trash folder in Settings before deleting messages.')
  if (!accounts[accountID]?.password&&!accounts[accountID]?.oauth) throw new Error('Add an IMAP password in Settings to read this mailbox.')
  const account = accounts[accountID]!
  const authorization=account.oauth?await oauthAccess(account.oauth,signal):{password:account.password!,lifetime:session.activationLifetimeSeconds};check()
  const credentials = { [account.id]: {
    host: account.imap_host, port: account.imap_port, security: account.imap_security, username: account.imap_username,...(account.imap_sasl?{sasl_mechanism:account.imap_sasl}:{}), ...('oauth_token' in authorization?{oauth_token:authorization.oauth_token}:{password:account.password!}),
  }}
  await activateCredentials({ lifetime_seconds: authorization.lifetime, accounts: credentials }, signal); check()
  return {check, account}
}
export async function access(accountID: string, signal: AbortSignal) {
  return (await accessAccount(accountID,signal)).check
}
function messagePath(ref: MessageRef) { return `/mailboxes/${encodeURIComponent(ref.accountID)}/messages/${ref.uid}` }
export async function prepareFolderReference(ref:MessageRef,signal:AbortSignal,explicitInbox=false){
  if(ref.folder&&ref.folder!=='INBOX'||explicitInbox)ref.folderID=await folderReference(ref.accountID,ref.folder||'INBOX',signal)
  else delete ref.folderID
}
export function messageQuery(ref: MessageRef) {
  const id=ref.folderID||(ref.folder&&ref.folder!=='INBOX'?cachedFolderReference(ref.accountID,ref.folder):undefined)
  if(ref.folder&&ref.folder!=='INBOX'&&!id)throw new Error('Reload the folder list before opening this message.')
  return `uid_validity=${ref.validity}${id?`&folder_id=${encodeURIComponent(id)}`:''}`
}
export function documentURL(ref: MessageRef, external: boolean) { return `/api/v1${messagePath(ref)}/document?${messageQuery(ref)}${external ? '&external=1' : ''}` }
export async function markMessageSeen(ref: MessageRef, signal: AbortSignal, seen = true) {
  const check = await access(ref.accountID, signal)
  await prepareFolderReference(ref,signal);check()
  await api(`${messagePath(ref)}/${seen ? 'seen' : 'unseen'}?${messageQuery(ref)}`, { method: 'POST', signal }); check()
}
export async function setMessageFlag(ref: MessageRef, flag: 'seen'|'flagged'|'answered', enabled: boolean, signal: AbortSignal) {
  const check=await access(ref.accountID,signal)
  await prepareFolderReference(ref,signal);check()
  await api(`${messagePath(ref)}/flag?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify({flag,enabled})});check()
}
export async function transferMessage(ref: MessageRef,destination: string,move: boolean,signal: AbortSignal,destinationAccount?:string){
  const check=await access(ref.accountID,signal)
  await prepareFolderReference(ref,signal);check()
  if(destinationAccount&&destinationAccount!==ref.accountID){await access(destinationAccount,signal);check()}
  await api(`${messagePath(ref)}/transfer?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify({destination,move,...(destinationAccount?{destination_account:destinationAccount}:{})})});check()
}
export async function manageFolder(accountID: string,action: FolderAction,name: string,newName: string,confirmation: string,signal: AbortSignal){
  const check=await access(accountID,signal)
  await api(`/mailboxes/${encodeURIComponent(accountID)}/folders`,{method:'POST',signal,body:JSON.stringify({action,name,new_name:newName,confirmation})});check()
}
export async function trashMessage(ref:MessageRef,signal:AbortSignal) {
  const {check,account}=await accessAccount(ref.accountID,signal,true)
  await prepareFolderReference(ref,signal);check()
  const target=account.folder_mapping?.trash
  const destination=target ? {trash_folder:target} : {trash_exclude:Object.entries(account.folder_mapping || {}).filter(([role,name])=>role!=='trash' && !!name).map(([,name])=>name!)}
  await api(`${messagePath(ref)}/trash?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify(destination)});check()
}
export async function messageSource(ref: MessageRef, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  await prepareFolderReference(ref,signal);check()
  const response = await apiResponse(`${messagePath(ref)}/source?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check(); return blob
}
export async function attachmentBlob(ref: MessageRef, attachment: Attachment, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  await prepareFolderReference(ref,signal);check()
  const response = await apiResponse(`${messagePath(ref)}/attachments/${encodeURIComponent(attachment.part)}?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check(); return blob
}
export async function listFolders(accountID: string, signal: AbortSignal) {
  return retryRead(async()=>{
  const check = await access(accountID, signal)
  const page = await api<FolderPage>(`/mailboxes/${encodeURIComponent(accountID)}/folders`, { signal }); check()
  rememberFolderReferences(accountID,page.folders||[])
  return page
  },signal)
}
export async function listMessages(accountID: string, signal: AbortSignal, before = 0, validity = 0, folder = 'INBOX', filters: MessageFilters = {}) {
  return retryRead(async()=>{
  const check = await access(accountID, signal)
  const id=folder!=='INBOX'?await folderReference(accountID,folder,signal):'';check()
  const params = [before ? `before=${before}&uid_validity=${validity}` : '', id ? `folder_id=${encodeURIComponent(id)}` : ''].filter(Boolean).join('&')
  const query = params ? `?${params}` : ''
  const search=Object.fromEntries(Object.entries(filters).filter(([,value])=>value?.trim()).map(([key,value])=>[key,value!.trim()]))
  const page = await api<MessagePage>(`/mailboxes/${encodeURIComponent(accountID)}/messages${Object.keys(search).length?'/search':''}${query}`, Object.keys(search).length?{method:'POST',signal,body:JSON.stringify(search)}:{ signal }); check()
  return page
  },signal)
}
export async function readMessage(ref: MessageRef, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  await prepareFolderReference(ref,signal);check()
  const message = await api<Message>(`${messagePath(ref)}?${messageQuery(ref)}`, { signal }); check()
  return message
}
export async function listMessageReferences(accountID:string,folder:string,validity:number,uids:number[],signal:AbortSignal,filters:MessageFilters={}){
  const check=await access(accountID,signal)
  const reference={accountID,folder,validity,uid:uids[0]||0};await prepareFolderReference(reference,signal);check()
  const page=await api<MessagePage>(`/mailboxes/${encodeURIComponent(accountID)}/messages/references?${messageQuery(reference)}`,{method:'POST',signal,body:JSON.stringify({uids:uids.slice(0,50),uid_validity:validity,filters})});check()
  return page
}
export async function downloadAttachment(ref: MessageRef, attachment: Attachment, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  await prepareFolderReference(ref,signal);check()
  const response = await apiResponse(`${messagePath(ref)}/attachments/${encodeURIComponent(attachment.part)}?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check()
  const url = URL.createObjectURL(blob)
  try {
    const anchor = document.createElement('a')
    anchor.href = url; anchor.download = Array.from(attachment.filename).map(char => char === '/' || char === '\\' || char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 ? '_' : char).join('') || 'attachment'
    document.body.append(anchor); anchor.click(); anchor.remove()
  } finally { URL.revokeObjectURL(url) }
}
