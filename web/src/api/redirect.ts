import { api, APIError } from './client'
import { activateSender, prepareSentCopy, type SendResult } from './messages'
import { access, messageQuery, prepareFolderReference, type MessageRef, type Address } from './reading'
export async function redirectMessage(ref: MessageRef, input: { smtp_config_id: string; to: Address[]; cc: Address[]; bcc: Address[]; sent_account_id?:string }, password?: string, signal = new AbortController().signal) {
  const receiving = await access(ref.accountID,signal)
  await prepareFolderReference(ref,signal);receiving()
  const sending = await activateSender(input.smtp_config_id,password)
  receiving(); sending()
  const {sent_account_id,...message}=input
  const copy=await prepareSentCopy(sent_account_id,input.smtp_config_id,()=>{receiving();sending()},signal)
  try {
    const result=await api<SendResult>(`/mailboxes/${encodeURIComponent(ref.accountID)}/messages/${ref.uid}/redirect?${messageQuery(ref)}`,{method:'POST',signal,body:JSON.stringify({...message,...(copy.input ? {sent_copy:copy.input} : {})})})
    if(copy.warning)result.sent_copy={status:'failed',message:copy.warning}
    return result
  }
  catch(error) { throw error instanceof APIError ? error : new APIError('SMTP_OUTCOME_UNKNOWN','Delivery could not be confirmed. Check your provider before redirecting again.',0,error) }
}
