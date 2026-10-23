import { onUnmounted, ref, watch } from 'vue'
import { listFolders, type FolderPage } from '../api/reading'
import { APIError } from '../api/client'
import { reconnectDelay, retryableRead, waitForRetry } from '../api/recovery'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'

export function useFolderSync() {
  const mailbox = useMailboxStore(); const session = useSessionStore()
  const folders = ref<Record<string, FolderPage>>({})
  const folderErrors = ref<Record<string, string>>({})
  const visible = ref(document.visibilityState !== 'hidden'); const reconnect = ref(0)
  const visibility = () => { visible.value = document.visibilityState !== 'hidden' }
  const online = () => { reconnect.value++ }
  const hide = () => { visible.value = false }
  document.addEventListener('visibilitychange', visibility)
  globalThis.addEventListener('online', online)
  globalThis.addEventListener('pagehide', hide)
  globalThis.addEventListener('pageshow', visibility)
  const stop = watch([() => session.unlocked, () => session.vaultEpoch, visible,
    () => mailbox.accounts.filter(account => account.sync_folders).map(account => account.id).join('|'),
    () => mailbox.refreshInterval, () => mailbox.refreshToken, reconnect], async (_, __, cleanup) => {
    const controller = new AbortController(); const signal = controller.signal
    cleanup(() => controller.abort())
    const accounts = mailbox.accounts.filter(account => account.sync_folders)
    const ids = new Set(accounts.map(account => account.id))
    folders.value = Object.fromEntries(Object.entries(folders.value).filter(([id]) => session.unlocked && ids.has(id)))
    folderErrors.value = {}
    if (!session.unlocked || !visible.value || !ids.size) return
    const halted = new Set<string>(); let failures = 0
    while (!signal.aborted) {
      let failed = false
      const queue = accounts.filter(account => !halted.has(account.id))
      if (!queue.length) return
      await Promise.all([0,1].map(async () => {
        while (queue.length && !signal.aborted) {
          const account = queue.shift()!
          try {
            const page = await listFolders(account.id, signal)
            if (!signal.aborted) { folders.value[account.id] = page; delete folderErrors.value[account.id] }
          } catch (error) {
            if (signal.aborted) return
            if (error instanceof APIError && error.status === 401) { session.clearLocalSession(); return }
            folderErrors.value[account.id] = (error instanceof Error ? error.message : 'Could not synchronize folders.') + (retryableRead(error) ? ' Retrying automatically…' : '')
            if (!retryableRead(error)) halted.add(account.id)
            else failed = true
          }
        }
      }))
      if (signal.aborted) return
      const delay = failed ? reconnectDelay(failures++) : mailbox.refreshInterval * 1000
      if (!failed) failures = 0
      await waitForRetry(delay,signal)
    }
  }, { immediate: true })
  onUnmounted(() => {
    stop(); folders.value = {}; folderErrors.value = {}
    document.removeEventListener('visibilitychange', visibility)
    globalThis.removeEventListener('online', online)
    globalThis.removeEventListener('pagehide', hide)
    globalThis.removeEventListener('pageshow', visibility)
  })
  return { folders, folderErrors }
}
