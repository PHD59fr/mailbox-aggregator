<script setup lang="ts">
import { t, formatDate, formatNumber, plural } from '../i18n'
import { computed, nextTick, ref, useId, watch } from 'vue'
import MailSelectionActions from '../components/MailSelectionActions.vue'
import type { FolderAction } from '../api/reading'
import { useRouter } from 'vue-router'
import { useMailActions, type MailAction } from '../stores/mailActions'
import AppHeader from '../components/AppHeader.vue'
import VaultUnlock from '../components/VaultUnlock.vue'
import AppIcon from '../components/AppIcon.vue'
import { useSessionStore } from '../stores/session'
import { useMailboxStore } from '../stores/mailboxes'
import { useInbox, type InboxRow } from '../composables/useInbox'
import MessageReader from '../components/MessageReader.vue'
import { presentFolders } from '../api/folders'
import FolderNavigation from '../components/FolderNavigation.vue'
import {useMailOrganization} from '../stores/organization'
import MessageOrganization from '../components/MessageOrganization.vue'
import {useWorkspaceDisplay} from '../composables/useWorkspaceDisplay'
import {accountColor} from '../api/workspaceDisplay'
import {useMailShortcuts} from '../composables/useMailShortcuts'
import {setMessageFlag} from '../api/reading'
import SearchHighlight from '../components/SearchHighlight.vue'
import QuotaStatus from '../components/QuotaStatus.vue'
import MailWorkspaceTools from '../components/MailWorkspaceTools.vue'
import {combinedViews,messageKey,type CombinedView} from '../api/mailSources'
const session = useSessionStore()
const mailbox = useMailboxStore()
useWorkspaceDisplay()
const organization=useMailOrganization()
watch(()=>[session.unlocked,session.userID,session.vaultEpoch],()=>{organization.clear();if(session.unlocked)void organization.load()},{immediate:true})
const router = useRouter(); const mailActions = useMailActions()
async function startAction(action: MailAction) {
  if (!selected.value || !message.value) return
  if(action==='draft'&&message.value.truncated){session.showNotice('This draft is too large to edit completely. Open it in another IMAP client.','warning');return}
  const account = mailbox.accounts.find(account => account.id === selected.value?.accountID)
  const smtpConfigID = account?.smtp_config_id
  mailActions.pending = { action, reference: { ...selected.value }, message: message.value, smtpConfigID, ownEmails: mailbox.accounts.map(account => account.imap_username), replyFromRecipient: account?.reply_from_recipient ?? false }
  await router.push({ path: '/compose', query: smtpConfigID ? { from: smtpConfigID } : {} })
}
const { rows, filter, folder, chooseView, sourceNotices, sources, folders, folderErrors, selected, message, reading, readError, downloading, marking, deleting, removeMessage, toggleSeen, loading, errors, hasMore, open, closeMessage, chooseMailbox, reload, loadMore, download } = useInbox()
const checked=ref<string[]>([])
const rowKey=(row:InboxRow)=>messageKey({accountID:row.accountID,folder:row.folder,validity:row.validity,uid:row.uid})
const isSelected=(row:InboxRow)=>!!selected.value&&messageKey(selected.value)===rowKey(row)
const selectedReferences=computed(()=>rows.value.filter(row=>checked.value.includes(rowKey(row))).map(row=>({accountID:row.accountID,uid:row.uid,validity:row.validity,...(row.folder?{folder:row.folder}:{})})))
const selectionFolders=computed(()=>folders.value[selectedReferences.value[0]?.accountID||'']?.folders||[])
watch([filter,folder,()=>mailbox.view,()=>mailbox.searchScope,()=>session.vaultEpoch],()=>{checked.value=[]})
watch(rows,()=>{const visible=new Set(rows.value.map(rowKey));checked.value=checked.value.filter(key=>visible.has(key))})
function actionsChanged(){checked.value=[];closeMessage();mailbox.refresh()}
const currentFolder = computed(()=>presentFolders(folders.value[filter.value]?.folders || [], mailbox.accounts.find(account=>account.id===filter.value)?.folder_mapping).find(item=>item.name===folder.value))
const folderParts = computed(()=>{
  if (!filter.value && mailbox.view !== 'inbox') return [t(combinedViews[mailbox.view])]
  if (folder.value === 'INBOX') return [t('Inbox')]
  if (currentFolder.value?.role) return [t(currentFolder.value.label)]
  const delimiter = currentFolder.value?.delimiter
  return delimiter ? folder.value.split(delimiter).filter(Boolean) : [folder.value]
})
const folderTitle = computed(()=>folderParts.value[folderParts.value.length-1] || folder.value)
const navigationID = useId()
const navigationOpen = ref(false)
const navigationToggle = ref<InstanceType<typeof globalThis.HTMLButtonElement> | null>(null)
const navigationLabel = computed(() => filter.value ? `${mailbox.accounts.find(account => account.id === filter.value)?.name || 'Mailbox'}${folder.value === 'INBOX' ? '' : ` / ${folderTitle.value}`}` : t(combinedViews[mailbox.view]))
const navigationUnseen = computed(() => filter.value ? currentFolder.value?.unseen ?? mailbox.statuses[filter.value]?.unseen ?? 0 : mailbox.unseen)
function selectMailbox(accountID: string, path = 'INBOX') { chooseMailbox(accountID, path); navigationOpen.value = false }
function selectView(view:CombinedView){chooseView(view);navigationOpen.value=false}
function closeNavigation() { navigationOpen.value = false; navigationToggle.value?.focus() }
watch(() => session.vaultEpoch, () => { navigationOpen.value = false })
function folderChanged(accountID: string, action: FolderAction, name: string, newName: string) {
  const delimiter = folders.value[accountID]?.folders.find(item => item.name === name)?.delimiter
  if (filter.value === accountID && (folder.value === name || delimiter && folder.value.startsWith(name + delimiter))) {
    if (action === 'rename') selectMailbox(accountID, newName + folder.value.slice(name.length))
    else if (action === 'delete') selectMailbox(accountID)
  }
  actionsChanged()
}
const selectedFolderRole = computed(()=>presentFolders(folders.value[selected.value?.accountID||'']?.folders||[],mailbox.accounts.find(account=>account.id===selected.value?.accountID)?.folder_mapping).find(item=>item.name===selected.value?.folder)?.role)
const trashDisabled = computed(()=>mailbox.accounts.find(account=>account.id===selected.value?.accountID)?.folder_mapping?.trash===null)
const readerPane = ref<{ scrollIntoView?: (options: { block: 'start'; behavior: 'smooth' }) => void } | null>(null)
async function openRow(row: InboxRow) {
  const pending = open(row)
  await nextTick()
  if (globalThis.innerWidth <= 1150) readerPane.value?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
  await pending
}
const sender = (row: InboxRow) => row.from.map(address => address.name || address.email).join(', ') || 'Unknown sender'
const shortDate = (date: string) => formatDate(date, {month:'short',day:'numeric'})
let keyboardIndex=-1
function stepMessage(step:number){if(!rows.value.length)return;const index=selected.value?rows.value.findIndex(row=>rowKey(row)===messageKey(selected.value!)):keyboardIndex;keyboardIndex=Math.max(0,Math.min(rows.value.length-1,index+step));void openRow(rows.value[keyboardIndex]!)}
async function starMessage(){if(!selected.value||!message.value)return;const reference={...selected.value},flagged=!message.value.flagged;try{await setMessageFlag(reference,'flagged',flagged,new globalThis.AbortController().signal);if(selected.value&&messageKey(selected.value)===messageKey(reference)&&message.value)message.value.flagged=flagged;mailbox.refresh()}catch(value){session.showNotice(value instanceof Error?value.message:'Could not update star.','warning')}}
useMailShortcuts({next:()=>stepMessage(1),previous:()=>stepMessage(-1),close:closeMessage,read:()=>{void toggleSeen()},star:()=>{void starMessage()},reply:()=>{if(selected.value)void startAction('reply')},compose:()=>{void router.push('/compose')},search:()=>globalThis.document.querySelector<InstanceType<typeof globalThis.HTMLInputElement>>('.mail-search input')?.focus()})
const retry = () => { const row = rows.value.find(row => selected.value && rowKey(row) === messageKey(selected.value)); if (row) void open(row) }
</script>
<template>
  <div class="app-shell">
    <AppHeader />
    <main v-if="session.loading" class="center-state">{{ t("Restoring session…") }}</main>
    <main v-else-if="!session.authenticated" class="center-state"><h1>{{ t("Session ended") }}</h1><RouterLink class="primary link-button" to="/login">{{ t("Sign in") }}</RouterLink></main>
    <main v-else-if="!session.unlocked" class="center-state"><VaultUnlock /></main>
    <main v-else>
      <div class="workspace-heading"><div><h1>{{ folderTitle }}</h1><p>{{ plural("{count} mailbox", "{count} mailboxes", mailbox.accounts.length) }} · {{ formatNumber(mailbox.unseen) }} {{ t("unread") }}</p></div></div>
      <div v-if="loading" class="mail-loading" role="status" aria-live="polite" aria-atomic="true"><AppIcon name="refresh" :size="19" /><div><h2>{{ mailbox.filtering ? t("Searching messages…") : t("Loading messages…") }}</h2><p>{{ mailbox.filtering ? t("Applying your search and filters.") : t("Fetching the latest messages.") }}</p></div></div>
      <MailWorkspaceTools :accountID="filter" :folder="folder" :folders="folders" @view="selectView" @mailbox="selectMailbox" />
      <section v-if="mailbox.filtersOpen" id="mail-filters" class="mail-filters" :aria-label="t('Mail filters')">
        <div class="mail-filters-heading"><h2>{{ t("Filter mail") }}</h2><span>{{ mailbox.filterCount }} {{ t("active") }}</span></div>
        <div class="mail-filter-fields">
          <label>{{ t("Read status") }}<select v-model="mailbox.readFilter"><option value="">{{ t("All messages") }}</option><option value="unread">{{ t("Unread") }}</option><option value="read">{{ t("Read") }}</option></select></label>
          <label for="filter-from">{{ t("From") }}<input id="filter-from" v-model="mailbox.senderFilter" maxlength="254" :placeholder="t('Sender name or address')"></label>
          <label for="filter-to">{{ t("Recipient (To / Cc)") }}<input id="filter-to" v-model="mailbox.recipientFilter" maxlength="254" :placeholder="t('Recipient name or address')"></label>
          <label for="filter-subject">{{ t("Subject contains") }}<input id="filter-subject" v-model="mailbox.subjectFilter" maxlength="512" :placeholder="t('Words in the subject')"></label>
          <label for="filter-since">{{ t("Received from") }}<input id="filter-since" v-model="mailbox.sinceFilter" type="date" :max="mailbox.untilFilter || undefined"></label>
          <label for="filter-until">{{ t("Received through") }}<input id="filter-until" v-model="mailbox.untilFilter" type="date" :min="mailbox.sinceFilter || undefined"></label>
        </div>
        <div class="mail-filter-footer"><p class="quiet">{{ t("Filters apply automatically in the selected mailbox and folder.") }}</p><button type="button" class="text-button" :disabled="!mailbox.filtering" @click="mailbox.clearFilters()">{{ t("Clear filters") }}</button></div>
      </section>
      <div class="mail-grid" :class="[{ 'has-selection': selected },`density-${mailbox.display.density}`,`layout-${mailbox.display.layout}`]">
        <aside class="accounts" :class="{ 'has-mailboxes': mailbox.accounts.length }">
          <button v-if="mailbox.accounts.length" ref="navigationToggle" type="button" class="mobile-mailbox-toggle" :aria-expanded="navigationOpen" :aria-controls="navigationID" @click="navigationOpen = !navigationOpen"><AppIcon name="folder" :size="18" /><span>{{ navigationLabel }}</span><b>{{ formatNumber(navigationUnseen) }} {{ t("unread") }}</b><AppIcon name="chevron" :size="16" :class="{ expanded: navigationOpen }" /></button>
          <div :id="navigationID" class="mailbox-navigation" :class="{ 'is-open': navigationOpen }" @keydown.esc="closeNavigation">
          <p class="section-label">{{ t("Mailboxes") }}</p>
          <button type="button" class="folder" :class="{ active: !filter && mailbox.view==='inbox' }" :aria-pressed="!filter && mailbox.view==='inbox'" @click="selectMailbox('')"><AppIcon name="inbox" :size="18" /><span>{{ t("All inboxes") }}</span><b>{{ formatNumber(mailbox.unseen) }}</b></button>
          <div v-if="mailbox.accounts.length" class="combined-views" :aria-label="t('Combined views')"><button v-for="view in (['unread','sent','drafts','flagged','all','custom','tagged','snoozed','recent'] as const)" :key="view" type="button" class="folder" :class="{active:!filter&&mailbox.view===view}" :aria-pressed="!filter&&mailbox.view===view" @click="selectView(view)">{{t(combinedViews[view])}}</button></div>
          <ul v-if="mailbox.accounts.length" class="mailbox-statuses">
            <li v-for="account in mailbox.accounts" :key="account.id">
              <button type="button" class="mailbox-choice" :class="{ active: filter === account.id && folder === 'INBOX' }" :aria-pressed="filter === account.id && folder === 'INBOX'" @click="selectMailbox(account.id)"><strong>{{ account.name }}</strong><span v-if="mailbox.statuses[account.id]?.error" class="error">{{ t(mailbox.statuses[account.id]?.error) }}</span><span v-else-if="mailbox.statuses[account.id]">{{ mailbox.statuses[account.id]?.mode === 'idle' ? t("Live") : t("Every {0}s", {"0": mailbox.refreshInterval}) }} · {{ mailbox.statuses[account.id]?.unseen }} {{ t("unread") }}</span><span v-else>{{ t("Connecting…") }}</span></button>
              <template v-if="account.sync_folders">
                <p v-if="folderErrors[account.id]" class="folder-error error" role="alert">{{ t(folderErrors[account.id]) }} <button type="button" class="text-button" @click="mailbox.refresh()">{{ t("Try again") }}</button></p>
                <p v-else-if="!folders[account.id]" class="quiet">{{ t("Synchronizing folders…") }}</p>
                <FolderNavigation v-if="folders[account.id]" :folders="folders[account.id]!.folders" :mapping="account.folder_mapping" :account-id="account.id" :account-name="account.name" :active="filter === account.id ? folder : ''" @select="selectMailbox(account.id,$event)" @changed="(action,name,newName) => folderChanged(account.id,action,name,newName)" />
                <QuotaStatus :accountID="account.id" />
                <p v-if="folders[account.id]?.truncated" class="quiet">{{ t("Showing the first 200 folders.") }}</p>
              </template>
            </li>
          </ul>
          <p v-else class="empty-note">{{ t("Add a mailbox in Settings to receive email.") }}</p>
          <RouterLink v-if="!mailbox.accounts.length" class="add-link" to="/settings#accounts"><AppIcon name="plus" :size="16" />{{ t("Add mailbox") }}</RouterLink>
          </div>
        </aside>
        <section class="message-list" :aria-busy="loading">
          <div class="pane-title"><div><p class="eyebrow">{{ filter ? mailbox.accounts.find(account => account.id === filter)?.name : t(combinedViews[mailbox.view]) }}<template v-if="folder !== 'INBOX'"> / {{ folderParts.join(' / ') }}</template></p><h2>{{ mailbox.filtering ? loading ? t("Searching…") : t("Search results") : t("Messages") }} <small v-if="rows.length">{{ formatNumber(rows.length) }}</small></h2></div><button class="icon-button" :aria-label="t('Refresh')" :disabled="loading" @click="mailbox.refresh()"><AppIcon name="refresh" :size="16" /></button></div>
          <div v-if="loading" class="message-list-loading" aria-hidden="true"><AppIcon name="refresh" :size="16" /><span>{{ mailbox.filtering ? t("Searching messages…") : t("Loading messages…") }}</span></div>
          <p v-if="mailbox.error" class="mailbox-error error" role="alert">{{ t(mailbox.error) }}</p>
          <p v-for="error in errors" :key="error" class="mailbox-error error" role="alert">{{ t(error) }}</p>
          <p v-for="notice in sourceNotices" :key="notice" class="quiet" role="status">{{t(notice)}}</p>
          <p v-if="sources.length>1 && (mailbox.view!=='inbox'||mailbox.searchScope==='all')" class="quiet">{{ t("Results merge loaded pages from") }} {{sources.length}} {{ t("folders. Load older messages to continue. Some messages can appear in several provider folders.") }}</p>
          <button v-if="errors.length" type="button" class="text-button" :disabled="loading" @click="reload">{{ t("Retry messages") }}</button>
          <MailSelectionActions :selection="selectedReferences" :folders="selectionFolders" :folders-by-account="Object.fromEntries(Object.entries(folders).map(([id,page])=>[id,page.folders]))" :scope="`${filter}:${folder}:${mailbox.view}:${mailbox.searchScope}`" @changed="actionsChanged" />
          <ul v-if="rows.length" class="inbox-messages" :aria-label="t('Messages')"><li v-for="row in rows" :key="rowKey(row)" :class="accountColor(mailbox.display.colors[row.accountID])"><label class="message-select"><input v-model="checked" type="checkbox" :value="rowKey(row)" :aria-label="t('Select {0}', {'0': row.subject || 'message'})" :disabled="checked.length>=50&&!checked.includes(rowKey(row))"></label><button type="button" class="message-row" :class="{ unread: !row.seen, selected: isSelected(row) }" :aria-pressed="isSelected(row)" @click="openRow(row)"><span class="message-row-top"><strong><SearchHighlight :text="sender(row)" :terms="[mailbox.search,mailbox.senderFilter]" /></strong><time :datetime="row.date">{{ shortDate(row.date) }}</time></span><span class="message-row-subject"><span v-if="row.flagged" :aria-label="t('Starred')">★ </span><span v-if="row.answered" :aria-label="t('Answered')">↩ </span><SearchHighlight :text="row.subject || '(No subject)'" :terms="[mailbox.search,mailbox.subjectFilter]" /></span><span v-if="!filter || mailbox.searchScope==='all'" class="message-account">{{ mailbox.accounts.find(account => account.id === row.accountID)?.name }}<template v-if="row.folder"> / {{row.folder}}</template></span></button></li></ul>
          <div v-else-if="!loading" class="empty-mail" role="status"><span class="empty-icon"><AppIcon :name="mailbox.filtering ? 'search' : 'inbox'" :size="30" /></span><h2>{{ errors.length ? t("Could not load messages") : mailbox.filtering ? t("No matching messages") : t("No messages") }}</h2><p>{{ mailbox.filtering ? t("Try another search or adjust your filters.") : mailbox.accounts.length ? t("Refresh or choose another folder.") : t("Add a mailbox in Settings to receive email.") }}</p></div>
          <div v-if="hasMore || (rows.length && loading)" class="inbox-pagination"><button type="button" class="secondary" :disabled="loading" @click="loadMore()">{{ loading ? t("Loading…") : t("Load older messages") }}</button></div>
        </section>
        <article ref="readerPane" class="reader" :aria-label="t('Message reader')"><MessageOrganization v-if="selected&&message" :reference="selected" @snoozed="closeMessage" /><MessageReader v-if="selected" :reference="selected" :message="message" :loading="reading" :error="readError" :downloading="downloading" :marking="marking" :deleting="deleting" :in-trash="selectedFolderRole === 'trash'" :trash-disabled="trashDisabled" @close="closeMessage" @retry="retry" @download="download" @action="startAction" @toggle-seen="toggleSeen" @trash="removeMessage" /><div v-else class="empty-mail"><span class="empty-icon"><AppIcon name="mail" :size="30" /></span><h2>{{ t("No message selected") }}</h2><p>{{ t("Choose a message from the list to read it.") }}</p></div></article>
      </div>
    </main>
  </div>
</template>
