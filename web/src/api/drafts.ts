import {api} from './client'
import {access,messageQuery,prepareFolderReference,type Address,type MessageRef} from './reading'
import {listAccounts} from './accounts'
export interface RemoteDraftInput{headers?:Record<string,string>;sender:Address;to:Address[];cc:Address[];bcc:Address[];subject:string;text:string;html:string;in_reply_to?:string;references?:string[];attachments:{filename:string;content_id?:string;content_type:string;data:string}[]}
export interface RemoteDraftReceipt{folder:string;uid:number;uid_validity:number;previous_removal?:'confirmed'|'unconfirmed'}
export async function saveRemoteDraft(accountID:string,input:RemoteDraftInput,previous:MessageRef|null,signal:AbortSignal){
 const check=await access(accountID,signal)
 const account=(await listAccounts())[accountID];check()
 if(!account||account.folder_mapping?.drafts===null)throw new Error('Choose an IMAP mailbox with an available Drafts folder.')
 if(previous&&previous.accountID!==accountID)throw new Error('The draft belongs to another account. Choose a new remote draft explicitly.')
 const folder=previous?.folder||account.folder_mapping?.drafts||''
 const exclude=Object.entries(account.folder_mapping||{}).filter(([role,path])=>role!=='drafts'&&!!path).map(([,path])=>path as string)
 const result=await api<RemoteDraftReceipt>(`/mailboxes/${encodeURIComponent(accountID)}/drafts`,{method:'POST',signal,body:JSON.stringify({...input,folder,exclude,...(previous?{previous_uid:previous.uid,previous_validity:previous.validity}:{})})});check();return result
}
export async function deleteRemoteDraft(reference:MessageRef,signal:AbortSignal){
 const check=await access(reference.accountID,signal)
 await prepareFolderReference(reference,signal,true);check()
 await api(`/mailboxes/${encodeURIComponent(reference.accountID)}/drafts/${reference.uid}?${messageQuery(reference)}`,{method:'DELETE',signal});check()
}
