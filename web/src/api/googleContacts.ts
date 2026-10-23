import { api } from './client'
import { oauthAccess } from './oauth'
import { useSessionStore } from '../stores/session'

export interface GoogleContact {name: string; email: string}
export async function googleContactPage(id: string, page: string, signal: AbortSignal) {
  const session = useSessionStore()
  const { vmk, vaultEpoch, userID, csrf } = session
  const check = () => {
    if (signal.aborted || !vmk || !session.unlocked || session.vmk !== vmk || session.vaultEpoch !== vaultEpoch || session.userID !== userID || session.csrf !== csrf) throw new DOMException('Contact lookup was cancelled.', 'AbortError')
  }
  check()
  const access = await oauthAccess({ id, provider: 'google' }, signal)
  check()
  const result = await api<{contacts: GoogleContact[]; next_page: string}>('/contacts/google', {
    method: 'POST', signal,
    body: JSON.stringify({access_token: access.oauth_token, page, lock_revision: session.serverLockRevision}),
  })
  check()
  return result
}
