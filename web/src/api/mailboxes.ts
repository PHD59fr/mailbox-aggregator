import { api, apiResponse } from './client'
import { listAccounts } from './accounts'
import { useSessionStore } from '../stores/session'
import {oauthAccess} from './oauth'
import { activateCredentials } from './credentials'

export interface MailboxStatus {
  account_id: string
  messages: number
  unseen: number
  uid_next: number
  uid_validity: number
  mode: 'idle' | 'polling'
  error?: string
  checked_at?: string
}

export async function watchMailboxes(interval: number, signal: AbortSignal, receive: (status: MailboxStatus) => void) {
  const session = useSessionStore()
  const { userID, vmk, vaultEpoch } = session
  const active = () => !signal.aborted && session.authenticated && session.userID === userID && session.vmk === vmk && session.vaultEpoch === vaultEpoch
  if (!vmk || !active()) return
  const accounts = await listAccounts()
  if (!active()) return
  const credentials:Record<string,{host:string;port:number;security:'tls'|'starttls';username:string;password?:string;oauth_token?:string;sasl_mechanism?:string}>={}
  let lifetime=session.activationLifetimeSeconds
  const queue=Object.values(accounts)
  await Promise.all([0,1].map(async()=>{
    while(queue.length&&active()){
      const account=queue.shift()!
      try{
        if(!account.password&&!account.oauth)throw new Error('Add an IMAP password in Settings.')
        const authorization=account.oauth?await oauthAccess(account.oauth,signal):{password:account.password,lifetime:session.activationLifetimeSeconds}
        if(!active())return
        lifetime=Math.min(lifetime,authorization.lifetime)
        credentials[account.id]={host:account.imap_host,port:account.imap_port,security:account.imap_security,username:account.imap_username,...(account.imap_sasl?{sasl_mechanism:account.imap_sasl}:{}),...('oauth_token' in authorization?{oauth_token:authorization.oauth_token}:{password:account.password})}
      }catch(value){if(active())receive({account_id:account.id,messages:0,unseen:0,uid_next:0,uid_validity:0,mode:'polling',error:value instanceof Error?value.message:'Could not activate this account.'})}
    }
  }))
  if(!active())return
  if (!Object.keys(credentials).length) return
  const activation = await activateCredentials({ lifetime_seconds: lifetime, accounts: credentials }, signal)
  if (!active()) return
  for (const [accountID, message] of Object.entries(activation?.account_errors || {})) {
    receive({ account_id: accountID, messages: 0, unseen: 0, uid_next: 0, uid_validity: 0, mode: 'polling', error: message })
  }
  const activated = Object.keys(credentials).filter(id => !activation?.account_errors?.[id])
  if (!activated.length) return
  const response = await apiResponse('/mailboxes/watch', { method: 'POST', signal, body: JSON.stringify({ refresh_interval_seconds: interval, accounts: activated }) })
  const watchID = response.headers.get('X-Watch-ID')
  const reader = response.body?.getReader()
  if (!watchID || !reader) { await reader?.cancel(); throw new Error('Mailbox streaming is unavailable.') }
  const cancel = () => { void reader.cancel().catch(() => {}) }
  signal.addEventListener('abort', cancel, { once: true })
  let lastEvent = Date.now()
  let pinging = false
  let presenceFailure: unknown
  const ping = globalThis.setInterval(() => {
    if (pinging || !active()) return
    pinging = true
    void api('/mailboxes/presence', { method: 'POST', signal, body: JSON.stringify({ watch_id: watchID }) })
      .catch(error => { presenceFailure = error; void reader.cancel() })
      .finally(() => { pinging = false })
  }, 15000)
  const watchdog = globalThis.setInterval(() => {
    if (!active() || Date.now() - lastEvent > 35000) {
      presenceFailure = new Error('Mailbox connection was interrupted. Reconnecting…')
      void reader.cancel()
    }
  }, 5000)
  try {
    const decoder = new TextDecoder()
    let buffer = ''
    while (active()) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      if (buffer.length > 65536) throw new Error('Invalid mailbox update.')
      let newline: number
      while ((newline = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1)
        if (!line.trim()) continue
        const event = JSON.parse(line) as MailboxStatus & { type: string }
        lastEvent = Date.now()
        if (event.type === 'status' && active()) receive(event)
      }
    }
    if (presenceFailure && active()) throw presenceFailure
  } finally {
    globalThis.clearInterval(ping)
    globalThis.clearInterval(watchdog)
    signal.removeEventListener('abort', cancel)
    await reader.cancel().catch(() => {})
  }
}
