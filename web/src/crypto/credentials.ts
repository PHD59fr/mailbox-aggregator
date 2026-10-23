import * as opaque from '@serenity-kit/opaque'
import { api, apiResponse, APIError, resourceRevision } from '../api/client'
import { useSessionStore } from '../stores/session'
import { reauthenticateWithOpaque } from './auth'
import { generateVMK, generateSalt, deriveKEK, encryptFile, decryptFile, unlockVault, type VaultMetadata, type EncryptedFile } from './vault'
import { aad, base64, fromBase64 } from './encoding'
import { migrateComposeDrafts } from './composeDraft'

import { type VaultConfig } from './keyHistory'
const fileNames = ['config', 'accounts', 'identities', 'smtp', 'preferences'] as const

export async function changePassword(login: string, currentPassword: string, nextPassword: string, rotate: boolean): Promise<void> {
  if (nextPassword.length < 12) throw new Error('Use at least 12 characters for your new password.')
  const session = useSessionStore()
  const { userID, vmk, csrf, vaultEpoch } = session
  const check = () => {
    if (!vmk || !session.authenticated || session.vmk !== vmk || session.userID !== userID || session.csrf !== csrf || session.vaultEpoch !== vaultEpoch) throw new Error('The vault or session changed. Sign in again.')
  }
  check()
  const auth = await reauthenticateWithOpaque(login, currentPassword)
  check(); session.accept(auth, login)
  const metadata = await api<VaultMetadata>('/vault/metadata')
  const verified = await unlockVault(currentPassword, userID, metadata)
  try { if (!vmk || !verified.every((value, index) => value === vmk[index])) throw new Error('The active vault key changed. Sign in again.') }
  finally { verified.fill(0) }
  check()
  await opaque.ready
  const client = opaque.client.startRegistration({ password: nextPassword })
  const response = await api<{ flow_id: string; registration_response: string }>('/auth/password/start', {
    method: 'POST', body: JSON.stringify({ login, registration_request: client.registrationRequest, rotate_vmk: rotate }),
  })
  check()
  const registration = opaque.client.finishRegistration({ clientRegistrationState: client.clientRegistrationState, registrationResponse: response.registration_response, password: nextPassword, identifiers: { client: login.trim().toLowerCase(), server: 'mail-aggregator' } })
  const nextVMK = rotate ? generateVMK() : vmk!
  const version = metadata.key_version + (rotate ? 1 : 0)
  const salt = generateSalt(); const nonce = crypto.getRandomValues(new Uint8Array(12))
  let kek: Uint8Array | undefined
  const draftKeys: Uint8Array[] = []
  try {
    kek = await deriveKEK(nextPassword, salt, metadata.kdf_parameters)
    check()
    const wrappingKey = await crypto.subtle.importKey('raw', kek as BufferSource, 'AES-GCM', false, ['encrypt'])
    const wrapped = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: aad(userID, 'vmk', 1, version) as BufferSource }, wrappingKey, nextVMK as BufferSource)
    const nextMetadata: VaultMetadata = { ...metadata, kdf_salt: base64(salt), wrap_nonce: base64(nonce), wrapped_vmk: base64(new Uint8Array(wrapped)), key_version: version }
    const files: Record<string, EncryptedFile> = {}
    if (rotate) {
      for (const name of fileNames) {
        const current = await apiResponse(`/vault/files/${name}`, { cache: 'no-store' })
        if (!resourceRevision(current)) throw new Error('Missing vault revision.')
        const plaintext = await decryptFile<unknown>(await current.json() as EncryptedFile, vmk!, userID, name)
        check()
        if (name === 'config') {
          if (!plaintext || typeof plaintext!=='object' || Array.isArray(plaintext)) throw new Error('Invalid vault configuration. Rotation was not committed.')
          const config = plaintext as VaultConfig
          if (config.legacy_vault_keys!==undefined&&!Array.isArray(config.legacy_vault_keys)) throw new Error('Invalid draft key history. Rotation was not committed.')
          const old = (config.legacy_vault_keys || []).filter(value => value.expires_at > Date.now())
          config.legacy_vault_keys = [...old, { key: base64(vmk!), expires_at: Date.now() + 7 * 86400000 }]
          for (const value of config.legacy_vault_keys) {
            const key = fromBase64(value.key)
            if (key.length !== 32) {key.fill(0);throw new Error('Invalid draft migration key.')}
            draftKeys.push(key)
          }
        }
        files[name] = await encryptFile(plaintext, nextVMK, userID, name, version)
        check()
      }
    }
    try {
      await api<void>('/auth/password/finish', { method: 'POST', body: JSON.stringify({ flow_id: response.flow_id, registration_record: registration.registrationRecord, vault_metadata: nextMetadata, files }) })
    } catch(error) {
      // A lost response can follow a durable commit. Never retry this flow or
      // continue editing with the old key until a fresh sign-in reconciles it.
      if (!(error instanceof APIError) || error.status >= 500 || error.code === 'CREDENTIAL_CHANGE_FAILED') session.clearLocalSession()
      throw error
    }
    // A key rotation preserves short-lived old keys inside the new encrypted
    // config so another device's seven-day browser drafts can migrate later.
    try { if (rotate) await migrateComposeDrafts(nextVMK, userID, () => true, draftKeys) }
    catch { /* The committed new encrypted config retains migration keys. Unlock retries migration. */ }
    finally { session.clearLocalSession() }
  } finally { kek?.fill(0); for(const key of draftKeys)key.fill(0);if (rotate) nextVMK.fill(0) }
}
