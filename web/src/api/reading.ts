import { api, apiResponse } from './client'
import { listAccounts } from './accounts'
import { useSessionStore } from '../stores/session'
import { activateCredentials } from './credentials'

export interface Address { name?: string; email: string }
export interface MessageSummary { uid: number; subject: string; from: Address[]; to: Address[]; cc: Address[]; bcc?:Address[];date: string; seen: boolean; flagged?: boolean; answered?: boolean; draft?:boolean; size: number }
export interface MessagePage { messages: MessageSummary[]; uid_validity: number; next_before: number; total: number }
export interface Message extends MessageSummary { uid_validity: number; text: string; html: string; attachments: Attachment[]; truncated: boolean; css?: string; reply_to?: Address[]; message_id?: string;in_reply_to?:string }
export interface Attachment { part: string; filename: string; content_type: string; size: number }
export interface MessageRef { accountID: string; uid: number; validity: number; folder?: string }
export interface MailFolder { name: string; delimiter: string; selectable: boolean; messages: number; unseen: number; uid_next: number; uid_validity: number; error?: string; role?: string; special_use?: boolean }
export interface FolderPage { folders: MailFolder[]; truncated: boolean }
export interface MessageFilters { search?: string; seen?: '' | 'read' | 'unread'; from?: string;to?:string;subject?:string;since?:string;until?:string }

async function accessAccount(accountID: string, signal: AbortSignal, requireTrash = false) {
  const session = useSessionStore()
  const { userID, vmk, vaultEpoch } = session
  const check = () => {
    if (signal.aborted || !vmk || !session.authenticated || session.userID !== userID || session.vmk !== vmk || session.vaultEpoch !== vaultEpoch) throw new DOMException('Reading was cancelled.', 'AbortError')
  }
  check()
  const accounts = await listAccounts(); check()
  if (requireTrash && accounts[accountID]?.folder_mapping?.trash === null) throw new Error('Trash is set to None for this mailbox. Choose a Trash folder in Settings before deleting messages.')
  if (!accounts[accountID]?.password) throw new Error('Add an IMAP password in Settings to read this mailbox.')
  const account = accounts[accountID]!
  const credentials = { [account.id]: {
    host: account.imap_host, port: account.imap_port, security: account.imap_security, username: account.imap_username, password: account.password!,
  }}
  await activateCredentials({ lifetime_seconds: session.activationLifetimeSeconds, accounts: credentials }, signal); check()
  return {check, account}
}
export async function access(accountID: string, signal: AbortSignal) {
  return (await accessAccount(accountID,signal)).check
}
function messagePath(ref: MessageRef) { return `/mailboxes/${encodeURIComponent(ref.accountID)}/messages/${ref.uid}` }
export function messageQuery(ref: MessageRef) { return `uid_validity=${ref.validity}${ref.folder && ref.folder !== 'INBOX' ? `&folder=${encodeURIComponent(ref.folder)}` : ''}` }
export function documentURL(ref: MessageRef, external: boolean) { return `/api/v1${messagePath(ref)}/document?${messageQuery(ref)}${external ? '&external=1' : ''}` }
export async function markMessageSeen(ref: MessageRef, signal: AbortSignal, seen = true) {
  const check = await access(ref.accountID, signal)
  await api(`${messagePath(ref)}/${seen ? 'seen' : 'unseen'}?${messageQuery(ref)}`, { method: 'POST', signal }); check()
}
export async function setMessageFlag(ref: MessageRef, flag: 'seen'|'flagged'|'answered', enabled: boolean, signal: AbortSignal) {
  const check=await access(ref.accountID,signal)
  await api(`${messagePath(ref)}/flag?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify({flag,enabled})});check()
}
export async function transferMessage(ref: MessageRef,destination: string,move: boolean,signal: AbortSignal,destinationAccount?:string){
  const check=await access(ref.accountID,signal)
  if(destinationAccount&&destinationAccount!==ref.accountID){await access(destinationAccount,signal);check()}
  await api(`${messagePath(ref)}/transfer?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify({destination,move,...(destinationAccount?{destination_account:destinationAccount}:{})})});check()
}
export async function manageFolder(accountID: string,action: string,name: string,newName: string,confirmation: string,signal: AbortSignal){
  const check=await access(accountID,signal)
  await api(`/mailboxes/${encodeURIComponent(accountID)}/folders`,{method:'POST',signal,body:JSON.stringify({action,name,new_name:newName,confirmation})});check()
}
export async function trashMessage(ref:MessageRef,signal:AbortSignal) {
  const {check,account}=await accessAccount(ref.accountID,signal,true)
  const target=account.folder_mapping?.trash
  const destination=target ? `&trash_folder=${encodeURIComponent(target)}` : Object.entries(account.folder_mapping || {}).filter(([role,name])=>role!=='trash' && !!name).map(([,name])=>`&trash_exclude=${encodeURIComponent(name!)}`).join('')
  await api(`${messagePath(ref)}/trash?${messageQuery(ref)}${destination}`,{method:'POST',signal});check()
}
export async function messageSource(ref: MessageRef, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  const response = await apiResponse(`${messagePath(ref)}/source?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check(); return blob
}
export async function attachmentBlob(ref: MessageRef, attachment: Attachment, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  const response = await apiResponse(`${messagePath(ref)}/attachments/${encodeURIComponent(attachment.part)}?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check(); return blob
}
export async function listFolders(accountID: string, signal: AbortSignal) {
  const check = await access(accountID, signal)
  const page = await api<FolderPage>(`/mailboxes/${encodeURIComponent(accountID)}/folders`, { signal }); check()
  return page
}
export async function listMessages(accountID: string, signal: AbortSignal, before = 0, validity = 0, folder = 'INBOX', filters: MessageFilters = {}) {
  const check = await access(accountID, signal)
  const params = [before ? `before=${before}&uid_validity=${validity}` : '', folder !== 'INBOX' ? `folder=${encodeURIComponent(folder)}` : '',
    filters.search?.trim() ? `search=${encodeURIComponent(filters.search.trim())}` : '', filters.seen ? `seen=${filters.seen}` : '', filters.from?.trim() ? `from=${encodeURIComponent(filters.from.trim())}` : '',
    ...(['to','subject','since','until'] as const).map(key=>filters[key]?.trim() ? `${key}=${encodeURIComponent(filters[key]!.trim())}` : '')].filter(Boolean).join('&')
  const query = params ? `?${params}` : ''
  const page = await api<MessagePage>(`/mailboxes/${encodeURIComponent(accountID)}/messages${query}`, { signal }); check()
  return page
}
export async function readMessage(ref: MessageRef, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  const message = await api<Message>(`${messagePath(ref)}?${messageQuery(ref)}`, { signal }); check()
  return message
}
export async function downloadAttachment(ref: MessageRef, attachment: Attachment, signal: AbortSignal) {
  const check = await access(ref.accountID, signal)
  const response = await apiResponse(`${messagePath(ref)}/attachments/${encodeURIComponent(attachment.part)}?${messageQuery(ref)}`, { signal })
  const blob = await response.blob(); check()
  const url = URL.createObjectURL(blob)
  try {
    const anchor = document.createElement('a')
    anchor.href = url; anchor.download = Array.from(attachment.filename).map(char => char === '/' || char === '\\' || char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 ? '_' : char).join('') || 'attachment'
    document.body.append(anchor); anchor.click(); anchor.remove()
  } finally { URL.revokeObjectURL(url) }
}
