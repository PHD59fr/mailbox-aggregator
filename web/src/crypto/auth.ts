import * as opaque from '@serenity-kit/opaque'
import { api, APIError, setCSRFToken } from '../api/client'
import { createVault, encryptFile } from './vault'
import { requireSecureContext } from './secureContext'

const identifiers = (login: string) => ({ client: login.trim().toLowerCase(), server: 'mail-aggregator' })
export interface AuthSession { user_id: string; state: 'AUTHENTICATED_LOCKED' | 'AUTHENTICATED_UNLOCKED'; csrf_token: string; lock_revision?: number; max_accounts?: number; idle_timeout_seconds?: number }
interface RegistrationStart { flow_id: string; registration_response: string; user_id: string }
interface LoginStart { flow_id: string; login_response: string }
export class MFARequiredError extends APIError {
  constructor(public flowID:string,public expiresIn:number){super('MFA_REQUIRED','Enter your authenticator or recovery code.',401)}
}

export async function registerWithOpaque(login: string, password: string): Promise<AuthSession> {
  requireSecureContext()
  await opaque.ready
  const normalized = login.trim().toLowerCase()
  const client = opaque.client.startRegistration({ password })
  const start = await api<RegistrationStart>('/auth/register/start', { method: 'POST', body: JSON.stringify({ login: normalized, registration_request: client.registrationRequest }) })
  const result = opaque.client.finishRegistration({ clientRegistrationState: client.clientRegistrationState, registrationResponse: start.registration_response, password, identifiers: identifiers(normalized) })
  const vault = await createVault(password, start.user_id)
  const files: Record<string, unknown> = {}
  for (const name of ['config', 'accounts', 'identities', 'smtp', 'preferences']) files[name] = await encryptFile(name === 'config' ? {} : [], vault.vmk, start.user_id, name)
  const session = await api<AuthSession>('/auth/register/finish', { method: 'POST', body: JSON.stringify({ flow_id: start.flow_id, registration_record: result.registrationRecord, vault_metadata: vault.metadata, files }) })
  setCSRFToken(session.csrf_token)
  return session
}

export async function loginWithOpaque(login: string, password: string): Promise<AuthSession> {
  requireSecureContext()
  await opaque.ready
  const normalized = login.trim().toLowerCase()
  const client = opaque.client.startLogin({ password })
  const start = await api<LoginStart>('/auth/login/start', { method: 'POST', body: JSON.stringify({ login: normalized, start_login_request: client.startLoginRequest }) })
  const result = opaque.client.finishLogin({ clientLoginState: client.clientLoginState, loginResponse: start.login_response, password, identifiers: identifiers(normalized) })
  if (!result) throw new Error('Authentication failed.')
  const session = await api<AuthSession|{mfa_required:true;flow_id:string;expires_in:number}>('/auth/login/finish', { method: 'POST', body: JSON.stringify({ flow_id: start.flow_id, finish_login_request: result.finishLoginRequest }) })
  if('mfa_required' in session)throw new MFARequiredError(session.flow_id,session.expires_in)
  setCSRFToken(session.csrf_token)
  return session
}

export async function reauthenticateWithOpaque(login: string, password: string): Promise<AuthSession> {
  await opaque.ready
  const normalized = login.trim().toLowerCase()
  const client = opaque.client.startLogin({ password })
  const start = await api<LoginStart>('/auth/reauth/start', { method: 'POST', body: JSON.stringify({ login: normalized, start_login_request: client.startLoginRequest }) })
  const result = opaque.client.finishLogin({ clientLoginState: client.clientLoginState, loginResponse: start.login_response, password, identifiers: identifiers(normalized) })
  if (!result) throw new Error('Authentication failed.')
  return api<AuthSession>('/auth/reauth/finish', { method: 'POST', body: JSON.stringify({ flow_id: start.flow_id, finish_login_request: result.finishLoginRequest }) })
}
