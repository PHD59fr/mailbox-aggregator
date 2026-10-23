import { onUnmounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { listAccounts } from '../api/accounts'
import { watchMailboxes } from '../api/mailboxes'
import { APIError } from '../api/client'
import { useSessionStore } from '../stores/session'
import { useMailboxStore } from '../stores/mailboxes'
import { reconnectDelay, retryableRead, waitForRetry } from '../api/recovery'

export function useMailboxMonitoring(enabled: () => boolean = () => true) {
  const session = useSessionStore()
  const mailbox = useMailboxStore()
  const { accounts } = storeToRefs(mailbox)
  const visible = ref(document.visibilityState !== 'hidden')
  const visibility = () => { visible.value = document.visibilityState !== 'hidden' }
  document.addEventListener('visibilitychange', visibility)
  const pagehide = () => { visible.value = false }
  const pageshow = () => { visibility() }
  const reconnect = ref(0)
  const online = () => { reconnect.value++ }
  globalThis.addEventListener('pagehide', pagehide)
  globalThis.addEventListener('pageshow', pageshow)
  globalThis.addEventListener('online', online)
  const stop = watch([() => enabled() && session.unlocked, () => session.userID, visible, () => mailbox.refreshInterval, () => mailbox.refreshToken, reconnect], async ([unlocked, userID, isVisible], _, cleanup) => {
    const controller = new AbortController()
    cleanup(() => { controller.abort(); mailbox.monitoring = false })
    if (!unlocked || !userID) { accounts.value = []; mailbox.clear(); return }
    if (!isVisible) { mailbox.monitoring = false; return }
    let failures = 0
    do {
      try {
        const saved = Object.values(await listAccounts())
        if (controller.signal.aborted) return
        // Do not replace unchanged identities on each reconnect: doing so
        // would restart the reader's list and folder synchronization too.
        if (JSON.stringify(accounts.value) !== JSON.stringify(saved)) accounts.value = saved
        mailbox.statuses = Object.fromEntries(Object.entries(mailbox.statuses).filter(([id]) => saved.some(account => account.id === id)))
        if (!saved.length) { mailbox.clear(); return }
        mailbox.error = ''; mailbox.monitoring = true
        await watchMailboxes(mailbox.refreshInterval, controller.signal, status => {
            if (controller.signal.aborted) return
            const previous = mailbox.statuses[status.account_id]
            if (!status.error) { failures = 0; mailbox.error = '' }
            if (!status.error && previous && previous.uid_validity === status.uid_validity && status.uid_next > previous.uid_next) {
              const account = accounts.value.find(account => account.id === status.account_id)
              session.showNotice(`New mail in ${account?.name || 'your inbox'}.`)
            }
            mailbox.statuses[status.account_id] = status.error && previous ? { ...previous, error: status.error, mode: status.mode } : status
        })
        if (controller.signal.aborted) return
        if (accounts.value.every(account => !account.password)) return
      } catch (error) {
        if (controller.signal.aborted) return
        if (error instanceof APIError && error.status === 401) { session.clearLocalSession(); return }
        mailbox.error = error instanceof Error ? error.message : 'Could not monitor your mailboxes.'
        mailbox.monitoring = false
        if (error instanceof APIError && !retryableRead(error)) return
      }
      mailbox.monitoring = false
      await waitForRetry(reconnectDelay(failures++), controller.signal)
    } while (!controller.signal.aborted)
  }, { immediate: true })
  onUnmounted(() => {
    stop()
    document.removeEventListener('visibilitychange', visibility)
    globalThis.removeEventListener('pagehide', pagehide)
    globalThis.removeEventListener('pageshow', pageshow)
    globalThis.removeEventListener('online', online)
    mailbox.clear()
  })
  return { accounts, mailbox, refresh: () => { mailbox.refresh() } }
}
