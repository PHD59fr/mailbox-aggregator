
import { t } from '../i18n'
import { readCollection } from './vaultCollection'
import { APIError } from './client'
import { resetCredentials } from './credentials'
import { validateFolderMapping } from './folders'

import { useSessionStore } from '../stores/session'
import {validOAuthReference,type OAuthReference} from './oauth'

export type FolderRole = 'sent' | 'drafts' | 'trash' | 'spam' | 'archive' | 'all' | 'outbox'
// Missing/empty = automatic, null = disabled, string = exact server path.
export type FolderMapping = Partial<Record<FolderRole, string | null>>

export interface Account {
  imap_sasl?:'SCRAM-SHA-256'|''
	 oauth?:OAuthReference
  id: string
  name: string
  imap_host: string
  imap_port: number
  imap_security: 'tls' | 'starttls'
  imap_username: string
  smtp_config_id?: string
  is_default: boolean
  password?: string
  mark_as_read?: boolean
  sync_folders?: boolean
  reply_from_recipient?: boolean
  save_sent?: boolean
  folder_mapping?: FolderMapping
}

export interface SMTPConfig {
	 oauth?:OAuthReference
  id: string
  name: string
  host: string
  port: number
  security: 'tls' | 'starttls'
  username: string
  is_default: boolean
  sender_name?: string
  sender_email?: string
  password?: string
  auto_bcc?: string
  reply_to?: string
  signatures?: Signature[]
  default_signature_id?: string
}
export interface Signature {id:string;name:string;text:string}

export interface AccountInput {
  imap_sasl?:'SCRAM-SHA-256'|''
	 oauth?:OAuthReference
  name: string
  imap_host: string
  imap_port: number
  imap_security: 'tls' | 'starttls'
  imap_username: string
  smtp_config_id?: string
  password?: string
  mark_as_read?: boolean
  sync_folders?: boolean
  reply_from_recipient?: boolean
  save_sent?: boolean
  folder_mapping?: FolderMapping
}

export interface SMTPConfigInput {
	 oauth?:OAuthReference
  name: string
  host: string
  port: number
  security: 'tls' | 'starttls'
  username: string
  sender_name?: string
  sender_email?: string
  password?: string
  auto_bcc?: string
  reply_to?: string
  signatures?: Signature[]
  default_signature_id?: string
}

function validateSender(input:SMTPConfigInput){
	if(input.oauth&&!validOAuthReference(input.oauth))throw new Error('Invalid OAuth grant reference.')
 if(input.auto_bcc&&(!/^[^\s<>@]+@[^\s<>@]+$/.test(input.auto_bcc)||input.auto_bcc.length>254))throw new Error('Enter a valid automatic Bcc address.')
 if(input.reply_to && (!/^[^\s<>@]+@[^\s<>@]+$/.test(input.reply_to)||input.reply_to.length>254))throw new Error('Enter a valid Reply-To address.')
 if(input.signatures){
  if(input.signatures.length>12||new Set(input.signatures.map(item=>item.id)).size!==input.signatures.length)throw new Error('Use at most 12 distinct signatures.')
  for(const item of input.signatures)if(!item.id||item.id.length>128||!item.name.trim()||item.name.length>128||item.text.length>16384)throw new Error('Invalid signature name or content.')
  if(input.default_signature_id&&!input.signatures.some(item=>item.id===input.default_signature_id))throw new Error('Choose a saved default signature.')
 }
}

export async function listAccounts(): Promise<Record<string, Account>> {
  return (await readCollection<Account>('accounts')).records
}

export async function createAccount(input: AccountInput): Promise<Account> {
	if(input.oauth&&!validOAuthReference(input.oauth))throw new Error('Invalid OAuth grant reference.')
  input = { ...input }
  if(input.imap_sasl && (input.imap_sasl!=='SCRAM-SHA-256'||input.oauth))throw new Error('Choose password authentication for SCRAM-SHA-256.')
  validateFolderMapping(input.folder_mapping)
  if (input.smtp_config_id) {
    const configs = await listSMTPConfigs()
    if (!Object.prototype.hasOwnProperty.call(configs, input.smtp_config_id)) throw new Error('SMTP configuration not found.')
  }
  const collection = await readCollection<Account>('accounts')
  if (Object.keys(collection.records).length >= useSessionStore().maxAccounts) throw new Error(t("Maximum number of mailboxes ({0}) reached.", {"0": useSessionStore().maxAccounts}))
  const account: Account = { ...input, id: crypto.randomUUID(), is_default: Object.keys(collection.records).length === 0 }
  collection.records[account.id] = account
  await collection.save()
  return account
}

export async function deleteAccount(id: string): Promise<void> {
  const collection = await readCollection<Account>('accounts')
  if (!Object.prototype.hasOwnProperty.call(collection.records, id)) throw new Error('Account not found.')
  // Drop activated provider secrets as well as the encrypted saved configuration.
  collection.assertUnlocked()
  await resetCredentials()
  delete collection.records[id]
  setFallbackDefault(collection.records)
  await collection.save()
}

export async function updateAccount(id: string, input: Omit<AccountInput, 'smtp_config_id'>): Promise<void> {
  if(input.oauth&&!validOAuthReference(input.oauth))throw new Error('Invalid OAuth grant reference.')
  input = { ...input }
  if(input.imap_sasl && (input.imap_sasl!=='SCRAM-SHA-256'||input.oauth))throw new Error('Choose password authentication for SCRAM-SHA-256.')
  validateFolderMapping(input.folder_mapping)
  const collection = await readCollection<Account>('accounts')
  const existing = collection.records[id]
  if (!existing) throw new Error('Mailbox not found. Reload your settings.')
  collection.records[id] = { ...existing, ...(input.imap_sasl===undefined?{}:{imap_sasl:input.imap_sasl}), name: input.name, imap_host: input.imap_host,
    imap_port: input.imap_port, imap_security: input.imap_security, imap_username: input.imap_username, password: input.password || existing.password,
    ...(input.mark_as_read === undefined ? {} : {mark_as_read:input.mark_as_read}),
    ...(input.sync_folders === undefined ? {} : {sync_folders:input.sync_folders}),
    ...(input.reply_from_recipient === undefined ? {} : {reply_from_recipient:input.reply_from_recipient}),
    ...(input.save_sent === undefined ? {} : {save_sent:input.save_sent}),
    ...(input.folder_mapping === undefined ? {} : {folder_mapping:{...input.folder_mapping}}) }
	if(input.oauth){collection.records[id]!.oauth=input.oauth;delete collection.records[id]!.password}
	else if(input.password)delete collection.records[id]!.oauth
  collection.assertUnlocked()
  await resetCredentials()
  await collection.save()
}

export async function listSMTPConfigs(): Promise<Record<string, SMTPConfig>> {
  return (await readCollection<SMTPConfig>('smtp')).records
}

export async function createSMTPConfig(input: SMTPConfigInput): Promise<SMTPConfig> {
  input = { ...input }
  validateSender(input)
  const collection = await readCollection<SMTPConfig>('smtp')
  if (Object.keys(collection.records).length >= useSessionStore().maxAccounts) throw new Error(t("Maximum number of sending identities ({0}) reached.", {"0": useSessionStore().maxAccounts}))
  const config: SMTPConfig = { ...input, id: crypto.randomUUID(), is_default: Object.keys(collection.records).length === 0 }
  collection.records[config.id] = config
  await collection.save()
  return config
}

export async function updateSMTPConfig(id: string, input: SMTPConfigInput): Promise<void> {
  input = { ...input }
  validateSender(input)
  const collection = await readCollection<SMTPConfig>('smtp')
  const existing = collection.records[id]
  if (!existing) throw new Error('Sender address not found. Reload your settings.')
  collection.records[id] = { ...existing, name: input.name, host: input.host, port: input.port,
    security: input.security, username: input.username, sender_name: input.sender_name ?? existing.sender_name,
    sender_email: input.sender_email ?? existing.sender_email, password: input.password || existing.password,
    ...(input.reply_to===undefined?{}:{reply_to:input.reply_to}),
	...(input.auto_bcc===undefined?{}:{auto_bcc:input.auto_bcc}),
    ...(input.signatures===undefined?{}:{signatures:input.signatures.map(item=>({...item}))}),
    ...(input.default_signature_id===undefined?{}:{default_signature_id:input.default_signature_id}) }
	if(input.oauth){collection.records[id]!.oauth=input.oauth;delete collection.records[id]!.password}
	else if(input.password)delete collection.records[id]!.oauth
  collection.assertUnlocked()
  await resetCredentials()
  await collection.save()
}

export async function linkSendingIdentity(accountID: string, smtpConfigID: string): Promise<void> {
  if (smtpConfigID) {
    const configs = await listSMTPConfigs()
    if (!Object.prototype.hasOwnProperty.call(configs, smtpConfigID)) throw new Error('Sending identity not found.')
  }
  const collection = await readCollection<Account>('accounts')
  const account = collection.records[accountID]
  if (!account) throw new Error('Receiving identity not found.')
  if (smtpConfigID) account.smtp_config_id = smtpConfigID
  else delete account.smtp_config_id
  await collection.save()
}

export async function deleteSMTPConfig(id: string): Promise<void> {
  const collection = await readCollection<SMTPConfig>('smtp')
  if (!Object.prototype.hasOwnProperty.call(collection.records, id)) throw new Error('SMTP configuration not found.')
  const accounts = await readCollection<Account>('accounts')
  const linked = Object.values(accounts.records).filter(account => account.smtp_config_id === id)
  collection.assertUnlocked()
  accounts.assertUnlocked()
  await resetCredentials()
  if (linked.length) {
    for (const account of linked) delete account.smtp_config_id
    await accounts.save()
  }
  delete collection.records[id]
  setFallbackDefault(collection.records)
  try {
    await collection.save()
  } catch (error) {
    if (!linked.length) throw error
    throw new APIError('SENDER_REMOVAL_INCOMPLETE', t("Linked mailboxes were kept and unlinked, but the sender could not be removed. {0}", {"0": error instanceof Error ? error.message : 'Reload and try again.'}), error instanceof APIError ? error.status : 0, error)
  }
}

function setFallbackDefault<T extends { is_default: boolean }>(records: Record<string, T>) {
  const remaining = Object.values(records)
  if (!remaining.some(record => record.is_default) && remaining[0]) remaining[0].is_default = true
}
