import { computed, onUnmounted, ref, watch } from 'vue'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import { listMessages, readMessage, downloadAttachment, markMessageSeen, trashMessage, type Message, type MessagePage, type MessageRef, type MessageSummary, type Attachment } from '../api/reading'
import { APIError } from '../api/client'
import { reconnectDelay, retryableRead } from '../api/recovery'
import { useFolderSync } from './useFolderSync'

export interface InboxRow extends MessageSummary { accountID: string; validity: number }
interface PageState { page?: MessagePage; loading: boolean; error: string }

export function useInbox() {
  const mailbox = useMailboxStore(); const session = useSessionStore()
  const pages = ref<Record<string, PageState>>({})
  const filter = ref('')
  const folder = ref('INBOX')
  const folderSync = useFolderSync()
  const selected = ref<MessageRef | null>(null)
  const message = ref<Message | null>(null)
  const reading = ref(false); const readError = ref(''); const downloading = ref('')
  const marking = ref(false)
  const deleting = ref(false)
  const searching = ref(false)
  let lists = new AbortController(); let reader = new AbortController()
  let reloadTimer: ReturnType<typeof setTimeout> | undefined
  const retries = new Map<string, { timer: ReturnType<typeof setTimeout>; attempt: number }>()
  let activeLoads = 0
  const retryQueue: Array<() => void> = []
  function drainRetries() { while (activeLoads < 2 && retryQueue.length) retryQueue.shift()!() }
  function clearRetries() { for (const retry of retries.values()) clearTimeout(retry.timer); retries.clear(); retryQueue.length = 0 }
  const visibleAccounts = computed(() => mailbox.accounts.filter(account => !filter.value || account.id === filter.value))
  const rows = computed<InboxRow[]>(() => visibleAccounts.value.flatMap(account => {
    const page = pages.value[account.id]?.page
    return (page?.messages || []).map(row => ({ ...row, accountID: account.id, validity: page!.uid_validity }))
  }).sort((a,b) => (Date.parse(b.date) || 0) - (Date.parse(a.date) || 0) || a.accountID.localeCompare(b.accountID) || b.uid - a.uid))
  const loading = computed(() => searching.value || visibleAccounts.value.some(account => pages.value[account.id]?.loading))
  const errors = computed(() => visibleAccounts.value.filter(account => pages.value[account.id]?.error).map(account => `${account.name}: ${pages.value[account.id]!.error}`))
  const hasMore = computed(() => visibleAccounts.value.some(account => !!pages.value[account.id]?.page?.next_before))

  function closeMessage() { reader.abort(); reader = new AbortController(); selected.value = null; message.value = null; readError.value = ''; reading.value = false; downloading.value = ''; marking.value = false; deleting.value = false }
  function reset() { searching.value = false; clearRetries(); lists.abort(); lists = new AbortController(); pages.value = {}; closeMessage() }
  async function load(accountID: string, more: boolean, signal: AbortSignal) {
    if (signal.aborted || !session.unlocked || pages.value[accountID]?.loading) return
    if (activeLoads >= 2) { retryQueue.push(() => { void load(accountID,more,signal) }); return }
    const previous = pages.value[accountID]?.page
    const attempt = retries.get(accountID)?.attempt ?? 0
    clearTimeout(retries.get(accountID)?.timer); retries.delete(accountID)
    if (more && !previous?.next_before) return
    activeLoads++
    pages.value[accountID] = { page: previous, loading: true, error: '' }
    try {
      const page = mailbox.filtering
        ? await listMessages(accountID, signal, more ? previous!.next_before : 0, more ? previous!.uid_validity : 0, folder.value, {search:mailbox.search.trim(),seen:mailbox.readFilter,from:mailbox.senderFilter.trim(),
          ...(mailbox.recipientFilter.trim() ? {to:mailbox.recipientFilter.trim()}:{}),...(mailbox.subjectFilter.trim() ? {subject:mailbox.subjectFilter.trim()}:{}),
          ...(mailbox.sinceFilter ? {since:mailbox.sinceFilter}:{}),...(mailbox.untilFilter ? {until:mailbox.untilFilter}:{})})
        : folder.value === 'INBOX'
        ? await listMessages(accountID, signal, more ? previous!.next_before : 0, more ? previous!.uid_validity : 0)
        : await listMessages(accountID, signal, more ? previous!.next_before : 0, more ? previous!.uid_validity : 0, folder.value)
      if (signal.aborted) return
      if (more && previous && previous.uid_validity === page.uid_validity) {
        const seen = new Set(previous.messages.map(row => row.uid))
        page.messages = [...previous.messages, ...page.messages.filter(row => !seen.has(row.uid))]
      }
      pages.value[accountID] = { page, loading: false, error: '' }
      if (selected.value?.accountID === accountID && selected.value.validity !== page.uid_validity) closeMessage()
    } catch (error) {
      if (signal.aborted) return
      if (error instanceof APIError && error.status === 401) { session.clearLocalSession(); return }
      const retry = retryableRead(error)
      pages.value[accountID] = { page: previous, loading: false, error: (error instanceof Error ? error.message : 'Could not load messages.') + (retry ? ' Retrying automatically…' : '') }
      if (retry) retries.set(accountID, { attempt: attempt + 1, timer: setTimeout(() => {
        if (!signal.aborted && document.visibilityState !== 'hidden') void load(accountID,more,signal)
      }, reconnectDelay(attempt)) })
    } finally { activeLoads--; drainRetries() }
  }
  async function batch(more = false) {
    const signal = lists.signal
    const queue = visibleAccounts.value.map(account => account.id)
    // Keep transient IMAP connections bounded even with several linked inboxes.
    await Promise.all([0,1].map(async () => {
      while (queue.length && !signal.aborted) { const id = queue.shift()!; if (!pages.value[id]?.loading) await load(id,more,signal) }
    }))
  }
  function reload() {
    searching.value = false
    clearTimeout(reloadTimer)
    clearRetries()
    lists.abort(); lists = new AbortController()
    for (const page of Object.values(pages.value)) page.loading = false
    if (session.unlocked) void batch()
  }
  function chooseMailbox(id: string, name = 'INBOX') {
    if (filter.value === id && folder.value === name) return
    filter.value = id; folder.value = name; pages.value = {}; closeMessage(); reload()
  }
  async function open(row: InboxRow) {
    closeMessage()
    const reference: MessageRef = { accountID: row.accountID, uid: row.uid, validity: row.validity, ...(folder.value === 'INBOX' ? {} : { folder: folder.value }) }
    selected.value = reference; reading.value = true
    const signal = reader.signal
    try {
      const result = await readMessage(reference,signal)
      if (!signal.aborted) {
        message.value = result
        if (!result.seen && mailbox.accounts.find(account => account.id === row.accountID)?.mark_as_read) {
          try {
            await markMessageSeen(reference, signal)
            if (!signal.aborted && message.value) { message.value.seen = true; for (const item of pages.value[row.accountID]?.page?.messages || []) if (item.uid === row.uid) item.seen = true }
          } catch (error) { if (!signal.aborted) readError.value = `Message opened, but could not mark it as read. ${error instanceof Error ? error.message : ''}` }
        }
      }
    } catch (error) {
      if (!signal.aborted) {
        readError.value = error instanceof Error ? error.message : 'Could not open this message.'
        if (error instanceof APIError && error.status === 401) session.clearLocalSession()
      }
    } finally { if (!signal.aborted) reading.value = false }
  }
  async function download(attachment: Attachment) {
    if (!selected.value || downloading.value) return
    const signal = reader.signal
    downloading.value = attachment.part; readError.value = ''
    try { await downloadAttachment(selected.value,attachment,signal) }
    catch (error) { if (!signal.aborted) readError.value = error instanceof Error ? error.message : 'Could not download this attachment.' }
    finally { if (!signal.aborted) downloading.value = '' }
  }
  async function toggleSeen() {
    if (!selected.value || !message.value || marking.value) return
    const reference = selected.value; const seen = !message.value.seen; const signal = reader.signal
    marking.value = true
    try {
      await markMessageSeen(reference,signal,seen)
      if (!signal.aborted && message.value) {
        message.value.seen = seen
        for (const item of pages.value[reference.accountID]?.page?.messages || []) if (item.uid === reference.uid) item.seen = seen
        // The IMAP status event refreshes counts/list once. Restarting the
        // entire watch here caused a second reload and a visible empty flash.
      }
    } catch (error) { if (!signal.aborted) readError.value = error instanceof Error ? error.message : 'Could not change the read status.' }
    finally { if (!signal.aborted) marking.value = false }
  }
  async function removeMessage() {
    if(!selected.value || !message.value || deleting.value) return
    const reference=selected.value;const signal=reader.signal
    deleting.value=true;readError.value=''
    try {
      await trashMessage(reference,signal)
      if(signal.aborted) return
      const page=pages.value[reference.accountID]?.page
      if(page) {
        page.messages=page.messages.filter(row=>row.uid!==reference.uid);page.total=Math.max(0,page.total-1)
        if(page.next_before===reference.uid) page.next_before=page.messages[page.messages.length-1]?.uid || 0
      }
      closeMessage();session.showNotice('Message moved to Trash.')
    } catch(error) {if(!signal.aborted) readError.value=error instanceof Error ? error.message : 'Could not move this message to Trash. Refresh before trying again.'}
    finally {if(!signal.aborted) deleting.value=false}
  }
  watch([() => session.unlocked, () => session.userID, () => session.vaultEpoch, () => mailbox.accounts.map(account => `${account.id}:${!!account.sync_folders}`).join('\n')], () => {
    reset()
    if (!mailbox.accounts.some(account => account.id === filter.value)) filter.value = ''
    if (!filter.value || !mailbox.accounts.find(account => account.id === filter.value)?.sync_folders) folder.value = 'INBOX'
    if (session.unlocked) void batch()
  }, { immediate: true })
  watch(() => mailbox.refreshToken, reload)
  const scheduleReload = () => {
    if (!session.unlocked) return
    clearTimeout(reloadTimer); reloadTimer = setTimeout(reload,750)
  }
  watch([() => mailbox.search, () => mailbox.readFilter, () => mailbox.senderFilter,()=>mailbox.recipientFilter,()=>mailbox.subjectFilter,()=>mailbox.sinceFilter,()=>mailbox.untilFilter], () => {
    // Cancel the old query immediately; delayed responses must not overwrite
    // a newer search. Keep the open message while loading a new query.
    clearTimeout(reloadTimer); clearRetries(); lists.abort(); lists = new AbortController(); pages.value = {}
    searching.value = session.unlocked
    if (session.unlocked) reloadTimer = setTimeout(reload,400)
  })
  watch([filter, folder, () => Object.entries(mailbox.statuses).filter(([id]) => !filter.value || id === filter.value).map(([id,status]) => `${id}:${status.uid_validity}:${status.uid_next}:${status.messages}:${status.unseen}:${status.error || ''}`).join('|')], ([id,name,status],[oldID,oldName,oldStatus]) => {
    if (name === 'INBOX' && id === oldID && name === oldName && status !== oldStatus) scheduleReload()
  })
  watch(() => {
    const status = folderSync.folders.value[filter.value]?.folders.find(item => item.name === folder.value)
    return folder.value !== 'INBOX' && status ? `${status.uid_validity}:${status.uid_next}:${status.messages}:${status.unseen}:${status.error || ''}` : ''
  }, (value, previous) => { if (value && previous) scheduleReload() })
  const resume = () => { if (session.unlocked && document.visibilityState !== 'hidden' && retries.size) reload() }
  document.addEventListener('visibilitychange', resume)
  globalThis.addEventListener('online', resume)
  onUnmounted(() => { document.removeEventListener('visibilitychange', resume); globalThis.removeEventListener('online', resume); clearTimeout(reloadTimer); reset() })
  return { rows, filter, folder, ...folderSync, selected, message, reading, readError, downloading, marking, deleting, removeMessage, toggleSeen, loading, errors, hasMore, open, closeMessage, chooseMailbox, reload, loadMore: () => batch(true), download }
}
