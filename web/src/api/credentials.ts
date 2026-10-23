import { api, apiResponse, APIError } from './client'
import { useSessionStore } from '../stores/session'

interface CredentialInput {
  host: string
  port: number
  security: 'tls' | 'starttls'
  username: string
  password: string
  sender_name?: string
  sender_email?: string
  reply_to?: string
}

interface Activation {
  lifetime_seconds: number
  accounts?: Record<string, CredentialInput>
  smtp?: Record<string, CredentialInput>
}

export interface ActivationResult {
 account_errors?: Record<string, string>
 smtp_errors?: Record<string, string>
}

const resetting = new WeakSet<object>()

function activationSession() {
  const session = useSessionStore()
  const { userID, csrf, vmk, vaultEpoch, serverLockRevision } = session
  const active = () => session.authenticated && !!vmk && session.vmk === vmk &&
    session.userID === userID && session.csrf === csrf && session.vaultEpoch === vaultEpoch
  if (!active()) throw new Error('Unlock your vault before activating mail credentials.')
  return { session, active, serverLockRevision }
}

export async function activateCredentials(input: Activation, signal?: AbortSignal) {
  const { session, active, serverLockRevision } = activationSession()
  try {
    return await api<ActivationResult>('/session/credentials', {
      method: 'POST', signal,
      body: JSON.stringify({ ...input, mode: 'merge', lock_revision: serverLockRevision }),
    })
  } catch (error) {
    // An older background request may conflict with this tab's own settings
    // reset. That response must not lock a tab already advancing its revision.
    if (active() && session.serverLockRevision === serverLockRevision && !resetting.has(session) &&
      error instanceof APIError && error.code === 'LOCK_REVISION_CONFLICT') session.lockLocal()
    throw error
  }
}

// Configuration edits clear provider secrets while keeping this tab's vault
// open. Adopt only the revision of our own reset, never a later remote lock.
export async function resetCredentials() {
  const { session, active, serverLockRevision } = activationSession()
  if (resetting.has(session)) throw new Error('Wait for the current account settings update.')
  resetting.add(session)
  try {
    const response = await apiResponse('/session/credentials', { method: 'DELETE' })
    if (!active()) throw new Error('The vault was locked. Unlock it and try again.')
    const raw = response.headers.get('X-Lock-Revision')
    const revision = raw === null ? NaN : Number(raw)
    if (!Number.isSafeInteger(revision) || revision !== serverLockRevision + 1 || session.serverLockRevision !== serverLockRevision) {
      session.lockLocal()
      throw new Error('The session changed. Unlock your vault and try again.')
    }
    session.serverLockRevision = revision
    await session.remember()
  } finally {
    resetting.delete(session)
  }
}
