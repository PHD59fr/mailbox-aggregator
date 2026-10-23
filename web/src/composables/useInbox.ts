
import { t } from '../i18n'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import { listMessageReferences, listMessages, readMessage, downloadAttachment, markMessageSeen, trashMessage, type Message, type MessagePage, type MessageRef, type MessageSummary, type Attachment } from '../api/reading'
import { APIError } from '../api/client'
import { reconnectDelay, retryableRead, retryRead } from '../api/recovery'
import { useFolderSync } from './useFolderSync'
import {useMailOrganization} from '../stores/organization'
import { combinedSources, sourceKey, messageKey, type MailSource, type CombinedView } from '../api/mailSources'

export interface InboxRow extends MessageSummary { accountID: string; validity: number; folder?: string }
interface PageState { page?: MessagePage; loading: boolean; error: string }

export function useInbox() {
  const mailbox = useMailboxStore(); const session = useSessionStore()
  const organization=useMailOrganization(),clock=ref(Date.now())
  const clockTimer=globalThis.setInterval(()=>{if(session.unlocked)clock.value=Date.now()},30000)
  const referenceView=computed(()=>!filter.value&&['tagged','snoozed','recent'].includes(mailbox.view))
  const marks=computed(()=>Object.values(organization.data.entries).filter(mark=>mailbox.accounts.some(account=>account.id===mark.reference.accountID)&&
    (mailbox.view==='tagged'?mark.tags.length>0&&(!mailbox.tagFilter||mark.tags.includes(mailbox.tagFilter)):mailbox.view==='snoozed'?(mark.snoozedUntil||0)>clock.value:mailbox.view==='recent'?!!mark.visitedAt:false)))
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
  let loadedScope = ''
  const scopeStamp = () => JSON.stringify([mailbox.view, mailbox.searchScope, sources.value,mailbox.display.windowDays||0,mailbox.display.windowLimit||0,referenceView.value?marks.value.map(mark=>mark.reference.uid):[],mailbox.tagFilter])
  let activeLoads = 0
  const retryQueue: Array<() => void> = []
  function drainRetries() { while (activeLoads < 2 && retryQueue.length) retryQueue.shift()!() }
  function clearRetries() { for (const retry of retries.values()) clearTimeout(retry.timer); retries.clear(); retryQueue.length = 0 }
  const visibleAccounts = computed(() => mailbox.accounts.filter(account => !filter.value || account.id === filter.value))
  const sourceSet = computed(() => {
    if (referenceView.value){
      const sources=[...new Map(marks.value.map(mark=>{const ref=mark.reference,source={accountID:ref.accountID,folder:ref.folder||'INBOX',validity:ref.validity};return [sourceKey(source),source]})).values()]
      return {sources:sources.slice(0,100),notices:sources.length>100?['This reference view is limited to 100 folders. Remove old references to see the rest.']:[]}
    }
    if (mailbox.searchScope === 'all' && mailbox.filtering) return combinedSources(mailbox.accounts, folderSync.folders.value, 'all')
    if (filter.value) return {sources: [{accountID: filter.value, folder: folder.value}], notices: []}
    return combinedSources(visibleAccounts.value, folderSync.folders.value, mailbox.view, mailbox.selectedFolders)
  })
  const sources = computed(() => sourceSet.value.sources)
  const sourceNotices = computed(() => sourceSet.value.notices)
  const keyFor = (reference: {accountID: string; folder?: string;validity?:number}) => sourceKey(sources.value.find(source=>source.accountID===reference.accountID&&source.folder===(reference.folder||'INBOX')&&(!source.validity||source.validity===reference.validity))||{accountID:reference.accountID,folder:reference.folder||'INBOX'})
  const rows = computed<InboxRow[]>(() => sources.value.flatMap(source => {
    const page = pages.value[sourceKey(source)]?.page
    return (page?.messages || []).map(row => ({ ...row, accountID: source.accountID, validity: page!.uid_validity, ...(source.folder === 'INBOX' ? {} : {folder: source.folder}) }))
  }).filter(row=>referenceView.value||(organization.data.entries[messageKey(row)]?.snoozedUntil||0)<=clock.value).sort((a,b) => (mailbox.view==='recent'?(organization.data.entries[messageKey(b)]?.visitedAt||0)-(organization.data.entries[messageKey(a)]?.visitedAt||0):0) || (Date.parse(b.date) || 0) - (Date.parse(a.date) || 0) || a.accountID.localeCompare(b.accountID) || (a.folder || 'INBOX').localeCompare(b.folder || 'INBOX') || b.uid - a.uid))
  const loading = computed(() => searching.value || sources.value.some(source => pages.value[sourceKey(source)]?.loading))
  const errors = computed(() => sources.value.filter(source => pages.value[sourceKey(source)]?.error).map(source => {
    const name = mailbox.accounts.find(account => account.id === source.accountID)?.name || source.accountID
    return `${name}${source.folder === 'INBOX' ? '' : ` / ${source.folder}`}: ${pages.value[sourceKey(source)]!.error}`
  }))
  const combinedMode = computed(()=>!filter.value && mailbox.view!=='inbox' || mailbox.searchScope==='all')
  const windowLimit = computed(()=>combinedMode.value && [50,100,250,500].includes(mailbox.display.windowLimit||0)?mailbox.display.windowLimit!:0)
  const windowSince = ()=>{
    if(!combinedMode.value||![7,30,90,365].includes(mailbox.display.windowDays||0))return ''
    const date=new Date();date.setDate(date.getDate()-mailbox.display.windowDays!);return date.toISOString().slice(0,10)
  }
  const hasMore = computed(() => sources.value.some(source => {const page=pages.value[sourceKey(source)]?.page;return !!page?.next_before && (!windowLimit.value||page.messages.length<windowLimit.value)}))

  function closeMessage() { reader.abort(); reader = new AbortController(); selected.value = null; message.value = null; readError.value = ''; reading.value = false; downloading.value = ''; marking.value = false; deleting.value = false }
  function reset() { searching.value = false; clearRetries(); lists.abort(); lists = new AbortController(); pages.value = {}; closeMessage() }
  async function load(source: MailSource, more: boolean, signal: AbortSignal) {
    const {accountID, folder: sourceFolder} = source
    const key = sourceKey(source)
    if (signal.aborted || !session.unlocked || pages.value[key]?.loading) return
    if (activeLoads >= 2) { retryQueue.push(() => { void load(source,more,signal) }); return }
    const previous = pages.value[key]?.page
    const attempt = retries.get(key)?.attempt ?? 0
    clearTimeout(retries.get(key)?.timer); retries.delete(key)
    if (more && (!previous?.next_before || windowLimit.value && previous.messages.length>=windowLimit.value)) return
    activeLoads++
    pages.value[key] = { page: previous, loading: true, error: '' }
    try {
      const filters = {search:mailbox.search.trim(),seen:mailbox.view === 'unread' && !filter.value ? 'unread' as const : mailbox.readFilter,from:mailbox.senderFilter.trim(),
        ...(mailbox.advancedSearch.trim()?{expression:mailbox.advancedSearch.trim()}:{}),
        ...(mailbox.starredFilter || mailbox.view === 'flagged' && !filter.value ? {flagged:'true' as const}:{}),
        ...(mailbox.recipientFilter.trim()?{to:mailbox.recipientFilter.trim()}:{}),...(mailbox.subjectFilter.trim()?{subject:mailbox.subjectFilter.trim()}:{}),
        ...([mailbox.sinceFilter,windowSince()].some(Boolean)?{since:[mailbox.sinceFilter,windowSince()].sort().slice(-1)[0]!}:{}),...(mailbox.untilFilter?{until:mailbox.untilFilter}:{})}
      const ids=referenceView.value?marks.value.filter(mark=>mark.reference.accountID===accountID&&(mark.reference.folder||'INBOX')===sourceFolder&&mark.reference.validity===source.validity).map(mark=>mark.reference.uid).filter(uid=>!more||uid<previous!.next_before).sort((a,b)=>b-a):[]
      const page = referenceView.value
        ? await listMessageReferences(accountID,sourceFolder,source.validity!,ids.slice(0,50),signal,filters)
        : mailbox.filtering || mailbox.view === 'unread' || mailbox.view === 'flagged' || windowSince()
        ? await listMessages(accountID,signal,more?previous!.next_before:0,more?previous!.uid_validity:0,sourceFolder,filters)
        : sourceFolder === 'INBOX'
        ? await listMessages(accountID,signal,more?previous!.next_before:0,more?previous!.uid_validity:0)
        : await listMessages(accountID,signal,more?previous!.next_before:0,more?previous!.uid_validity:0,sourceFolder)
      if(referenceView.value)page.next_before=ids.length>50?ids[49]!:0
      if (signal.aborted) return
      if (more && previous && previous.uid_validity === page.uid_validity) {
        const seen = new Set(previous.messages.map(row => row.uid))
        page.messages = [...previous.messages, ...page.messages.filter(row => !seen.has(row.uid))]
      }
      if(windowLimit.value && page.messages.length>windowLimit.value)page.messages=page.messages.slice(0,windowLimit.value)
      pages.value[key] = { page, loading: false, error: '' }
      if (selected.value?.accountID === accountID && (selected.value.folder || 'INBOX') === sourceFolder && selected.value.validity !== page.uid_validity) closeMessage()
    } catch (error) {
      if (signal.aborted) return
      if (error instanceof APIError && error.status === 401) { session.clearLocalSession(); return }
      const retry = retryableRead(error)
      pages.value[key] = { page: previous, loading: false, error: (error instanceof Error ? error.message : 'Could not load messages.') + (retry ? ' Retrying automatically…' : '') }
      if (retry) retries.set(key, { attempt: attempt + 1, timer: setTimeout(() => {
        if (!signal.aborted && document.visibilityState !== 'hidden') void load(source,more,signal)
      }, reconnectDelay(attempt)) })
    } finally { activeLoads--; drainRetries() }
  }
  async function batch(more = false) {
    const signal = lists.signal
    const queue = [...sources.value]
    // Keep transient IMAP connections bounded even with several linked inboxes.
    await Promise.all([0,1].map(async () => {
      while (queue.length && !signal.aborted) { const source = queue.shift()!; if (!pages.value[sourceKey(source)]?.loading) await load(source,more,signal) }
    }))
  }
  function reload() {
    loadedScope = scopeStamp()
    searching.value = false
    clearTimeout(reloadTimer)
    clearRetries()
    lists.abort(); lists = new AbortController()
    for (const page of Object.values(pages.value)) page.loading = false
    if (session.unlocked) void batch()
  }
  function chooseMailbox(id: string, name = 'INBOX') {
    if (filter.value === id && folder.value === name && mailbox.view === 'inbox' && mailbox.searchScope === 'current') return
    mailbox.view = 'inbox'; mailbox.searchScope = 'current'
    filter.value = id; folder.value = name; pages.value = {}; closeMessage(); reload()
  }
  async function open(row: InboxRow) {
    closeMessage()
    const reference: MessageRef = { accountID: row.accountID, uid: row.uid, validity: row.validity, ...(row.folder ? { folder: row.folder } : {}) }
    selected.value = reference; reading.value = true
    const signal = reader.signal
    try {
      const result = await retryRead(()=>readMessage(reference,signal),signal,()=>{if(!signal.aborted)readError.value='Temporary connection problem. Retrying automatically…'})
      if (!signal.aborted) {
        readError.value=''
        message.value = result
        void organization.remember(reference).catch(()=>session.showNotice('The message opened, but its recent-history reference could not be saved.','warning'))
        if (!result.seen && mailbox.accounts.find(account => account.id === row.accountID)?.mark_as_read) {
          try {
            await markMessageSeen(reference, signal)
            if (!signal.aborted && message.value) { message.value.seen = true; for (const item of pages.value[keyFor(row)]?.page?.messages || []) if (item.uid === row.uid) item.seen = true }
          } catch (error) { if (!signal.aborted) readError.value = t("Message opened, but could not mark it as read. {0}", {"0": error instanceof Error ? error.message : ''}) }
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
        for (const item of pages.value[keyFor(reference)]?.page?.messages || []) if (item.uid === reference.uid) item.seen = seen
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
      const page=pages.value[keyFor(reference)]?.page
      if(page) {
        page.messages=page.messages.filter(row=>row.uid!==reference.uid);page.total=Math.max(0,page.total-1)
        if(page.next_before===reference.uid) page.next_before=page.messages[page.messages.length-1]?.uid || 0
      }
      closeMessage();session.showNotice('Message moved to Trash.')
    } catch(error) {if(!signal.aborted) readError.value=error instanceof Error ? error.message : 'Could not move this message to Trash. Refresh before trying again.'}
    finally {if(!signal.aborted) deleting.value=false}
  }
  watch([() => session.unlocked, () => session.userID, () => session.vaultEpoch, () => mailbox.accounts.map(account => `${account.id}:${!!account.sync_folders}:${JSON.stringify(account.folder_mapping || {})}`).join('\n')], () => {
    reset()
    if (!mailbox.accounts.some(account => account.id === filter.value)) filter.value = ''
    if (!filter.value || !mailbox.accounts.find(account => account.id === filter.value)?.sync_folders) folder.value = 'INBOX'
    loadedScope = scopeStamp()
    if (session.unlocked) void batch()
  }, { immediate: true })
  watch(() => mailbox.refreshToken, reload)
  const scheduleReload = () => {
    if (!session.unlocked) return
    clearTimeout(reloadTimer); reloadTimer = setTimeout(reload,750)
  }
  watch([() => mailbox.search, () => mailbox.advancedSearch, () => mailbox.readFilter, () => mailbox.senderFilter,()=>mailbox.recipientFilter,()=>mailbox.subjectFilter,()=>mailbox.sinceFilter,()=>mailbox.untilFilter,()=>mailbox.starredFilter], () => {
    // Cancel the old query immediately; delayed responses must not overwrite
    // a newer search. Keep the open message while loading a new query.
    clearTimeout(reloadTimer); clearRetries(); lists.abort(); lists = new AbortController(); pages.value = {}
    searching.value = session.unlocked
    if (session.unlocked) reloadTimer = setTimeout(reload,400)
  })
  function chooseView(view: CombinedView) {
    filter.value = ''; folder.value = 'INBOX'; mailbox.view = view; mailbox.searchScope = 'current'
    pages.value = {}; closeMessage(); reload()
  }
  watch(scopeStamp, value => {
    if (value !== loadedScope) {
      const keys = new Set(sources.value.map(sourceKey))
      pages.value = Object.fromEntries(Object.entries(pages.value).filter(([key]) => keys.has(key)))
      closeMessage(); reload()
    }
  })
  watch(() => sources.value.map(source => {
    const status = source.folder === 'INBOX' ? mailbox.statuses[source.accountID] : folderSync.folders.value[source.accountID]?.folders.find(item => item.name === source.folder)
    return status ? `${sourceKey(source)}:${status.uid_validity}:${status.uid_next}:${status.messages}:${status.unseen}:${status.error || ''}` : ''
  }).join('|'), (value, previous) => { if (value !== previous) scheduleReload() })

  const resume = () => { if (session.unlocked && document.visibilityState !== 'hidden' && retries.size) reload() }
  document.addEventListener('visibilitychange', resume)
  globalThis.addEventListener('online', resume)
  onUnmounted(() => { document.removeEventListener('visibilitychange', resume); globalThis.removeEventListener('online', resume); clearTimeout(reloadTimer); globalThis.clearInterval(clockTimer); reset() })
  return { rows, filter, folder, sources, sourceNotices, chooseView, ...folderSync, selected, message, reading, readError, downloading, marking, deleting, removeMessage, toggleSeen, loading, errors, hasMore, open, closeMessage, chooseMailbox, reload, loadMore: () => batch(true), download }
}
