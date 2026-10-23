
import { t } from '../i18n'
import { derivePasswordKey } from '../crypto/argon2'
import { useSessionStore } from '../stores/session'
import { api, apiResponse, resourceRevision, APIError } from '../api/client'
import { resetCredentials } from '../api/credentials'
import { validateFolderMapping } from '../api/folders'
import { base64, fromBase64 } from '../crypto/encoding'
import { decryptFile, encryptFile, type EncryptedFile } from '../crypto/vault'
import {mergeCSV,type CSVAccounts} from '../api/accountCSV'
import type {Account,SMTPConfig} from '../api/accounts'

const collections = ['accounts', 'smtp', 'preferences'] as const
const maxExportBytes = 16 * 1024 * 1024
export interface ExportBundle {
  version: 1
  exported_at: string
  accounts: Record<string, unknown>
  smtp: Record<string, unknown>
  preferences: Record<string, unknown>
}
interface ExportEnvelope {
  version: 1
  algorithm: 'AES-256-GCM'
  kdf: 'argon2id'
  kdf_salt: string
  kdf_params: { memory_kib: number; iterations: number; parallelism: number; key_length: number }
  nonce: string
  ciphertext: string
}

async function deriveKey(password: string, salt: Uint8Array): Promise<CryptoKey> {
  const raw = await derivePasswordKey(password, salt)
  try { return await crypto.subtle.importKey('raw', raw as BufferSource, 'AES-GCM', false, ['encrypt', 'decrypt']) }
  finally { raw.fill(0) }
}

export async function encryptExportBundle(bundle: ExportBundle, password: string): Promise<string> {
  const plaintext = new TextEncoder().encode(JSON.stringify(bundle))
  if (plaintext.length > maxExportBytes) throw new Error('Configuration export is too large.')
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const nonce = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveKey(password, salt)
  try {
    const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce }, key, plaintext)
    return JSON.stringify({ version: 1, algorithm: 'AES-256-GCM', kdf: 'argon2id', kdf_salt: base64(salt),
      kdf_params: { memory_kib: 65536, iterations: 3, parallelism: 1, key_length: 32 }, nonce: base64(nonce),
      ciphertext: base64(new Uint8Array(ciphertext)) } satisfies ExportEnvelope)
  } finally { plaintext.fill(0) }
}

export async function decryptExportBundle(data: string, password: string): Promise<ExportBundle> {
  if (data.length > maxExportBytes * 2) throw new Error('Configuration export is too large.')
  const envelope = JSON.parse(data) as ExportEnvelope | null
  const params = envelope?.kdf_params
  if (!envelope || envelope.version !== 1 || envelope.algorithm !== 'AES-256-GCM' || envelope.kdf !== 'argon2id' ||
    params?.memory_kib !== 65536 || params.iterations !== 3 || params.parallelism !== 1 || params.key_length !== 32 ||
    typeof envelope.kdf_salt !== 'string' || typeof envelope.nonce !== 'string' || typeof envelope.ciphertext !== 'string') {
    throw new Error('Unsupported export format or KDF parameters.')
  }
  const salt = fromBase64(envelope.kdf_salt)
  const nonce = fromBase64(envelope.nonce)
  const ciphertext = fromBase64(envelope.ciphertext)
  if (salt.length !== 16 || nonce.length !== 12 || ciphertext.length < 16) throw new Error('Invalid export envelope.')
  const key = await deriveKey(password, salt)
  const plaintext = new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: nonce as BufferSource }, key, ciphertext as BufferSource))
  try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(plaintext)) as ExportBundle }
  finally { plaintext.fill(0) }
}

function context() {
  const session = useSessionStore()
  const { vmk, userID, csrf, vaultEpoch } = session
  const check = () => {
    if (!vmk || !session.authenticated || session.vmk !== vmk || session.userID !== userID || session.csrf !== csrf || session.vaultEpoch !== vaultEpoch) {
      throw new Error('The vault or session changed. Unlock it and try again.')
    }
  }
  check()
  return { session, vmk: vmk!, userID, check }
}

async function readFiles(check: () => void) {
  return Promise.all(collections.map(async name => {
    const response = await apiResponse(`/vault/files/${name}`, { cache: 'no-store' })
    const revision = resourceRevision(response)
    const envelope = await response.json() as EncryptedFile
    check()
    if (!revision) throw new Error('Missing vault revision. Reload and try again.')
    return { name, revision, envelope }
  }))
}

export async function exportUserData(password: string): Promise<string> {
  const { session, vmk, userID, check } = context()
  const files = await readFiles(check)
  const records = await Promise.all(files.map(async file => {
    const data = await decryptFile(file.envelope, vmk, userID, file.name)
    check()
    return [file.name, data]
  }))
  const bundle = { version: 1, exported_at: new Date().toISOString(), ...Object.fromEntries(records) } as ExportBundle
  const encrypted = await encryptExportBundle(validate(bundle, session.maxAccounts), password)
  check()
  return encrypted
}

function object(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}
function records(value: unknown): Record<string, unknown> {
  if (Array.isArray(value)) {
    const entries = value.map(item => {
      if (!object(item) || typeof item.id !== 'string') throw new Error('Invalid configuration record.')
      return [item.id, item] as const
    })
    if (new Set(entries.map(([id]) => id)).size !== entries.length) throw new Error('Duplicate configuration record.')
    return Object.fromEntries(entries)
  }
  if (!object(value)) throw new Error('Invalid configuration collection.')
  return value
}
function validate(bundle: ExportBundle, limit: number): ExportBundle {
  if (!object(bundle) || bundle.version !== 1 || typeof bundle.exported_at !== 'string') throw new Error('Invalid export bundle.')
  const normalized = { ...bundle, accounts: records(bundle.accounts), smtp: records(bundle.smtp), preferences: records(bundle.preferences) }
  for (const name of collections) {
    for (const [id, item] of Object.entries(normalized[name])) {
      if (!id || id.length > 128 || ['__proto__', 'constructor', 'prototype'].includes(id) || !object(item) || item.id !== id) throw new Error('Invalid configuration identifier.')
    }
  }
  for (const name of ['accounts', 'smtp'] as const) {
    if (Object.keys(normalized[name]).length > limit) throw new Error(t("This instance allows at most {0} identities per collection.", {"0": limit}))
    for (const item of Object.values(normalized[name]) as Record<string, unknown>[]) {
      const prefix = name === 'accounts' ? 'imap_' : ''
      if (typeof item.name !== 'string' || typeof item[`${prefix}host`] !== 'string' || !item[`${prefix}host`] ||
        !Number.isInteger(item[`${prefix}port`]) || Number(item[`${prefix}port`]) < 1 || Number(item[`${prefix}port`]) > 65535 ||
        !['tls', 'starttls'].includes(String(item[`${prefix}security`])) || typeof item[`${prefix}username`] !== 'string' ||
        typeof item.is_default !== 'boolean' || (item.password !== undefined && typeof item.password !== 'string')) throw new Error('Invalid mail identity in export.')
      if (name === 'accounts') {
        if (item.folder_mapping !== undefined && !object(item.folder_mapping)) throw new Error('Invalid folder mapping.')
        for (const key of ['mark_as_read','sync_folders','reply_from_recipient','save_sent']) if (item[key] !== undefined && typeof item[key] !== 'boolean') throw new Error('Invalid mailbox preference.')
        validateFolderMapping(item.folder_mapping as Parameters<typeof validateFolderMapping>[0])
        if (item.smtp_config_id && !Object.prototype.hasOwnProperty.call(normalized.smtp, String(item.smtp_config_id))) throw new Error('An imported mailbox refers to a missing sender.')
      }
    }
  }
  const privacy = normalized.preferences['mail-reader']
  if (object(privacy) && (typeof privacy.block_external !== 'boolean' || !Array.isArray(privacy.addresses) || !Array.isArray(privacy.domains) ||
    ![...privacy.addresses, ...privacy.domains].every(value => typeof value === 'string'))) throw new Error('Invalid image privacy preferences.')
  return normalized
}

export async function importUserData(data: string, password: string): Promise<void> {
  const { session, vmk, userID, check } = context()
  const bundle = validate(await decryptExportBundle(data, password), session.maxAccounts)
  check()
  const current = await readFiles(check)
  const files = Object.fromEntries(await Promise.all(current.map(async file => {
    const encrypted = await encryptFile(bundle[file.name], vmk, userID, file.name, file.envelope.key_version)
    check()
    return [file.name, { data: encrypted, revision: file.revision }]
  })))
  // Invalidate pending activations before replacing provider settings.
  await resetCredentials()
  check()
  try { await api<void>('/vault/import', { method: 'POST', body: JSON.stringify({ files }) }) }
  catch (error) {
    if (error instanceof APIError && error.code === 'REVISION_CONFLICT') throw error
    throw new APIError('IMPORT_UNCONFIRMED', 'Import could not be confirmed. Reload and inspect settings before importing again.', error instanceof APIError ? error.status : 0, error)
  }
  // Import revokes every session; the next login loads only the new settings.
  session.clearLocalSession()
}

// Reuse the existing three-collection transaction. Only ciphertext is sent;
// concurrent changes fail through the original revisions, without partial rows.
export async function importAccountCSV(parsed:CSVAccounts){
 const {session,vmk,userID,check}=context()
 const current=await readFiles(check)
 const decoded=Object.fromEntries(await Promise.all(current.map(async file=>{
  const value=await decryptFile(file.envelope,vmk,userID,file.name);check();return [file.name,value]
 })))
 const merged=mergeCSV({accounts:records(decoded.accounts) as Record<string,Account>,smtp:records(decoded.smtp) as Record<string,SMTPConfig>},parsed,session.maxAccounts)
 if(!merged.added)return merged
 decoded.accounts=merged.accounts;decoded.smtp=merged.smtp
 const files=Object.fromEntries(await Promise.all(current.map(async file=>{
  const data=await encryptFile(decoded[file.name],vmk,userID,file.name,file.envelope.key_version);check();return [file.name,{data,revision:file.revision}]
 })))
 const body=JSON.stringify({files})
 if(new TextEncoder().encode(body).length>2*1024*1024)throw new Error('The encrypted import exceeds the request limit. No changes were saved.')
 await resetCredentials();check()
 try{await api('/vault/import',{method:'POST',body})}
 catch(error){if(error instanceof APIError&&error.code==='REVISION_CONFLICT')throw error;throw new APIError('IMPORT_UNCONFIRMED','Import could not be confirmed. Sign in and inspect settings before retrying.',error instanceof APIError?error.status:0,error)}
 session.clearLocalSession();return merged
}
