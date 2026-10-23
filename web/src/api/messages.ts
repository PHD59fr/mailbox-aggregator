import { api, APIError } from './client'
import { listAccounts, listSMTPConfigs } from './accounts'
import { useSessionStore } from '../stores/session'
import { activateCredentials } from './credentials'

export interface MessageInput {
  smtp_config_id: string
  to: { email: string; name?: string }[]
  cc: { email: string; name?: string }[]
  bcc: { email: string; name?: string }[]
  subject: string
  text: string
  html?: string
  in_reply_to?: string
  references?: string[]
  attachments?: { filename: string; content_type: string; data: string }[]
  sent_account_id?: string
}

export interface SendResult { status: string; message_id: string; sent_copy?: {status: 'saved' | 'failed'; message?: string; account_id?: string} }
export interface SentCopyInput {account_id:string;folder:string;exclude:string[]}

export async function prepareSentCopy(accountID:string|undefined,smtpID:string,check:()=>void,signal?:AbortSignal):Promise<{input?:SentCopyInput;warning?:string}> {
  if (!accountID) return {}
  const session = useSessionStore()
  try {
    const account=(await listAccounts())[accountID];check()
    if (!account || account.smtp_config_id!==smtpID || account.save_sent===false || account.folder_mapping?.sent===null) throw new Error('Sent copies are disabled or this mailbox is no longer linked.')
    if (!account.password) throw new Error('Add an IMAP password to the linked mailbox.')
    await activateCredentials({lifetime_seconds:session.activationLifetimeSeconds,accounts:{[account.id]:{host:account.imap_host,port:account.imap_port,security:account.imap_security,username:account.imap_username,password:account.password}}},signal);check()
    return {input:{account_id:account.id,folder:account.folder_mapping?.sent || '',exclude:Object.entries(account.folder_mapping||{}).filter(([role,path])=>role!=='sent' && !!path).map(([,path])=>path as string)}}
  } catch(error) {
    check()
    return {warning:`The message was sent, but its Sent copy could not be saved. ${error instanceof Error ? error.message : 'Check the linked mailbox settings.'}`}
  }
}

export async function activateSender(smtpConfigID: string, password?: string) {
  const session = useSessionStore()
  const { userID, vmk, vaultEpoch } = session
  const check = () => {
    if (!session.authenticated || !vmk || session.vmk !== vmk || session.userID !== userID || session.vaultEpoch !== vaultEpoch) throw new Error('Unlock your vault before sending.')
  }
  check()
  const config = (await listSMTPConfigs())[smtpConfigID]
  if (!config?.sender_email) throw new Error('Select a sending identity with a sender address.')
  const secret = password || config.password
  if (!secret) throw new Error('Enter the SMTP password for this sending identity.')
  check()
  await activateCredentials({ lifetime_seconds: session.activationLifetimeSeconds, smtp: {
    [config.id]: { host: config.host, port: config.port, security: config.security, username: config.username, password: secret, sender_name: config.sender_name || '', sender_email: config.sender_email,...(config.reply_to?{reply_to:config.reply_to}:{}) },
  } })
  check()
  return check
}

export async function sendMessage(input: MessageInput, password?: string) {
  const check = await activateSender(input.smtp_config_id,password)
  const {sent_account_id,...message}=input
  const copy=await prepareSentCopy(sent_account_id,input.smtp_config_id,check)
  check()
  try {
    const result=await api<SendResult>('/messages/send', { method: 'POST', body: JSON.stringify({...message,...(copy.input ? {sent_copy:copy.input} : {})}) })
    if(copy.warning)result.sent_copy={status:'failed',message:copy.warning}
    return result
  }
  catch (error) { throw error instanceof APIError ? error : new APIError('SMTP_OUTCOME_UNKNOWN', 'Delivery could not be confirmed. Check your SMTP provider before sending again.', 0, error) }
}
