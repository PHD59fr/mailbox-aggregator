import { api, apiResponse } from './client'
import { listAccounts } from './accounts'
import { useSessionStore } from '../stores/session'
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
  const credentials = Object.fromEntries(Object.values(accounts).filter(account => account.password).map(account => [account.id, {
    host: account.imap_host, port: account.imap_port, security: account.imap_security, username: account.imap_username, password: account.password!,
  }]))
  for (const account of Object.values(accounts)) if (!account.password) receive({ account_id: account.id, messages: 0, unseen: 0, uid_next: 0, uid_validity: 0, mode: 'polling', error: 'Add an IMAP password in Settings.' })
  if (!Object.keys(credentials).length) return
  const activation = await activateCredentials({ lifetime_seconds: session.activationLifetimeSeconds, accounts: credentials }, signal)
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
