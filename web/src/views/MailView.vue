<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import MailSelectionActions from '../components/MailSelectionActions.vue'
import FolderManager from '../components/FolderManager.vue'
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
const session = useSessionStore()
const mailbox = useMailboxStore()
const router = useRouter(); const mailActions = useMailActions()
async function startAction(action: MailAction) {
  if (!selected.value || !message.value) return
  if(action==='draft'&&message.value.truncated){session.showNotice('This draft is too large to edit completely. Open it in another IMAP client.','warning');return}
  const account = mailbox.accounts.find(account => account.id === selected.value?.accountID)
  const smtpConfigID = account?.smtp_config_id
  mailActions.pending = { action, reference: { ...selected.value }, message: message.value, smtpConfigID, ownEmails: mailbox.accounts.map(account => account.imap_username), replyFromRecipient: account?.reply_from_recipient ?? false }
  await router.push({ path: '/compose', query: smtpConfigID ? { from: smtpConfigID } : {} })
}
const { rows, filter, folder, folders, folderErrors, selected, message, reading, readError, downloading, marking, deleting, removeMessage, toggleSeen, loading, errors, hasMore, open, closeMessage, chooseMailbox, loadMore, download } = useInbox()
const checked=ref<string[]>([])
const rowKey=(row:InboxRow)=>`${row.accountID}:${row.validity}:${row.uid}`
const selectedReferences=computed(()=>rows.value.filter(row=>checked.value.includes(rowKey(row))).map(row=>({accountID:row.accountID,uid:row.uid,validity:row.validity,folder:folder.value})))
const selectionFolders=computed(()=>folders.value[selectedReferences.value[0]?.accountID||'']?.folders||[])
watch([filter,folder,()=>session.vaultEpoch],()=>{checked.value=[]})
watch(rows,()=>{const visible=new Set(rows.value.map(rowKey));checked.value=checked.value.filter(key=>visible.has(key))})
function actionsChanged(){checked.value=[];closeMessage();mailbox.refresh()}
const currentFolder = computed(()=>presentFolders(folders.value[filter.value]?.folders || [], mailbox.accounts.find(account=>account.id===filter.value)?.folder_mapping).find(item=>item.name===folder.value))
const folderParts = computed(()=>{
  if (folder.value === 'INBOX') return ['Inbox']
  if (currentFolder.value?.role) return [currentFolder.value.label]
  const delimiter = currentFolder.value?.delimiter
  return delimiter ? folder.value.split(delimiter).filter(Boolean) : [folder.value]
})
const folderTitle = computed(()=>folderParts.value[folderParts.value.length-1] || folder.value)
const trashDisabled = computed(()=>mailbox.accounts.find(account=>account.id===selected.value?.accountID)?.folder_mapping?.trash===null)
const readerPane = ref<{ scrollIntoView?: (options: { block: 'start'; behavior: 'smooth' }) => void } | null>(null)
async function openRow(row: InboxRow) {
  const pending = open(row)
  await nextTick()
  if (globalThis.innerWidth <= 1150) readerPane.value?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
  await pending
}
const sender = (row: InboxRow) => row.from.map(address => address.name || address.email).join(', ') || 'Unknown sender'
const shortDate = (date: string) => new Date(date).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
const retry = () => { const row = rows.value.find(row => row.accountID === selected.value?.accountID && row.uid === selected.value.uid); if (row) void open(row) }
</script>
<template>
  <div class="app-shell">
    <AppHeader />
    <main v-if="session.loading" class="center-state">Restoring session…</main>
    <main v-else-if="!session.authenticated" class="center-state"><h1>Session ended</h1><RouterLink class="primary link-button" to="/login">Sign in</RouterLink></main>
    <main v-else-if="!session.unlocked" class="center-state"><VaultUnlock /></main>
    <main v-else>
      <div class="workspace-heading"><div><h1>{{ folderTitle }}</h1><p>{{ mailbox.accounts.length }} {{ mailbox.accounts.length === 1 ? 'mailbox' : 'mailboxes' }} · {{ mailbox.unseen }} unread</p></div></div>
      <div v-if="loading" class="mail-loading" role="status" aria-live="polite" aria-atomic="true"><AppIcon name="refresh" :size="19" /><div><h2>{{ mailbox.filtering ? 'Searching messages…' : 'Loading messages…' }}</h2><p>{{ mailbox.filtering ? 'Applying your search and filters.' : 'Fetching the latest messages.' }}</p></div></div>
      <section v-if="mailbox.filtersOpen" id="mail-filters" class="mail-filters" aria-label="Mail filters">
        <div class="mail-filters-heading"><h2>Filter mail</h2><span>{{ mailbox.filterCount }} active</span></div>
        <div class="mail-filter-fields">
          <label>Read status<select v-model="mailbox.readFilter"><option value="">All messages</option><option value="unread">Unread</option><option value="read">Read</option></select></label>
          <label for="filter-from">From<input id="filter-from" v-model="mailbox.senderFilter" maxlength="254" placeholder="Sender name or address"></label>
          <label for="filter-to">Recipient (To / Cc)<input id="filter-to" v-model="mailbox.recipientFilter" maxlength="254" placeholder="Recipient name or address"></label>
          <label for="filter-subject">Subject contains<input id="filter-subject" v-model="mailbox.subjectFilter" maxlength="512" placeholder="Words in the subject"></label>
          <label for="filter-since">Received from<input id="filter-since" v-model="mailbox.sinceFilter" type="date" :max="mailbox.untilFilter || undefined"></label>
          <label for="filter-until">Received through<input id="filter-until" v-model="mailbox.untilFilter" type="date" :min="mailbox.sinceFilter || undefined"></label>
        </div>
        <div class="mail-filter-footer"><p class="quiet">Filters apply automatically in the selected mailbox and folder.</p><button type="button" class="text-button" :disabled="!mailbox.filtering" @click="mailbox.clearFilters()">Clear filters</button></div>
      </section>
      <div class="mail-grid" :class="{ 'has-selection': selected }">
        <aside class="accounts">
          <p class="section-label">Mailboxes</p>
          <button type="button" class="folder" :class="{ active: !filter }" :aria-pressed="!filter" @click="chooseMailbox('')"><AppIcon name="inbox" :size="18" /><span>All inboxes</span><b>{{ mailbox.unseen }}</b></button>
          <ul v-if="mailbox.accounts.length" class="mailbox-statuses">
            <li v-for="account in mailbox.accounts" :key="account.id">
              <button type="button" class="mailbox-choice" :class="{ active: filter === account.id && folder === 'INBOX' }" :aria-pressed="filter === account.id && folder === 'INBOX'" @click="chooseMailbox(account.id)"><strong>{{ account.name }}</strong><span v-if="mailbox.statuses[account.id]?.error" class="error">{{ mailbox.statuses[account.id]?.error }}</span><span v-else-if="mailbox.statuses[account.id]">{{ mailbox.statuses[account.id]?.mode === 'idle' ? 'Live' : `Every ${mailbox.refreshInterval}s` }} · {{ mailbox.statuses[account.id]?.unseen }} unread</span><span v-else>Connecting…</span></button>
              <template v-if="account.sync_folders">
                <p v-if="folderErrors[account.id]" class="folder-error error" role="alert">{{ folderErrors[account.id] }}</p>
                <p v-else-if="!folders[account.id]" class="quiet">Synchronizing folders…</p>
                <FolderNavigation v-if="folders[account.id]" :folders="folders[account.id]!.folders" :mapping="account.folder_mapping" :account-id="account.id" :account-name="account.name" :active="filter === account.id ? folder : ''" @select="chooseMailbox(account.id,$event)" />
                <p v-if="folders[account.id]?.truncated" class="quiet">Showing the first 200 folders.</p>
                <FolderManager v-if="folders[account.id]" :accountID="account.id" :folders="folders[account.id]!.folders" @changed="actionsChanged" />
              </template>
            </li>
          </ul>
          <p v-else class="empty-note">Add a mailbox in Settings to receive email.</p>
          <RouterLink v-if="!mailbox.accounts.length" class="add-link" to="/settings#accounts"><AppIcon name="plus" :size="16" />Add mailbox</RouterLink>
        </aside>
        <section class="message-list" :aria-busy="loading">
          <div class="pane-title"><div><p class="eyebrow">{{ filter ? mailbox.accounts.find(account => account.id === filter)?.name : 'All inboxes' }}<template v-if="folder !== 'INBOX'"> / {{ folderParts.join(' / ') }}</template></p><h2>{{ mailbox.filtering ? loading ? 'Searching…' : 'Search results' : 'Messages' }} <small v-if="rows.length">{{ rows.length }}</small></h2></div><button class="icon-button" aria-label="Refresh" :disabled="loading" @click="mailbox.refresh()"><AppIcon name="refresh" :size="16" /></button></div>
          <div v-if="loading" class="message-list-loading" aria-hidden="true"><AppIcon name="refresh" :size="16" /><span>{{ mailbox.filtering ? 'Searching messages…' : 'Loading messages…' }}</span></div>
          <p v-if="mailbox.error" class="mailbox-error error" role="alert">{{ mailbox.error }}</p>
          <p v-for="error in errors" :key="error" class="mailbox-error error" role="alert">{{ error }}</p>
          <MailSelectionActions :selection="selectedReferences" :folders="selectionFolders" :folders-by-account="Object.fromEntries(Object.entries(folders).map(([id,page])=>[id,page.folders]))" :scope="`${filter}:${folder}`" @changed="actionsChanged" />
          <ul v-if="rows.length" class="inbox-messages" aria-label="Messages"><li v-for="row in rows" :key="`${row.accountID}:${row.validity}:${row.uid}`"><label class="message-select"><input v-model="checked" type="checkbox" :value="rowKey(row)" :aria-label="`Select ${row.subject || 'message'}`" :disabled="checked.length>=50&&!checked.includes(rowKey(row))"></label><button type="button" class="message-row" :class="{ unread: !row.seen, selected: selected?.accountID === row.accountID && selected?.uid === row.uid }" :aria-pressed="selected?.accountID === row.accountID && selected?.uid === row.uid" @click="openRow(row)"><span class="message-row-top"><strong>{{ sender(row) }}</strong><time :datetime="row.date">{{ shortDate(row.date) }}</time></span><span class="message-row-subject"><span v-if="row.flagged" aria-label="Starred">★ </span><span v-if="row.answered" aria-label="Answered">↩ </span>{{ row.subject || '(No subject)' }}</span><span v-if="!filter && mailbox.accounts.length > 1" class="message-account">{{ mailbox.accounts.find(account => account.id === row.accountID)?.name }}</span></button></li></ul>
          <div v-else-if="!loading" class="empty-mail" role="status"><span class="empty-icon"><AppIcon :name="mailbox.filtering ? 'search' : 'inbox'" :size="30" /></span><h2>{{ errors.length ? 'Could not load messages' : mailbox.filtering ? 'No matching messages' : 'No messages' }}</h2><p>{{ mailbox.filtering ? 'Try another search or adjust your filters.' : mailbox.accounts.length ? 'Refresh or choose another folder.' : 'Add a mailbox in Settings to receive email.' }}</p></div>
          <div v-if="rows.length && (hasMore || loading)" class="inbox-pagination"><button type="button" class="secondary" :disabled="loading" @click="loadMore()">{{ loading ? 'Loading…' : 'Load older messages' }}</button></div>
        </section>
        <article ref="readerPane" class="reader" aria-label="Message reader"><MessageReader v-if="selected" :reference="selected" :message="message" :loading="reading" :error="readError" :downloading="downloading" :marking="marking" :deleting="deleting" :in-trash="currentFolder?.role === 'trash'" :trash-disabled="trashDisabled" @close="closeMessage" @retry="retry" @download="download" @action="startAction" @toggle-seen="toggleSeen" @trash="removeMessage" /><div v-else class="empty-mail"><span class="empty-icon"><AppIcon name="mail" :size="30" /></span><h2>No message selected</h2><p>Choose a message from the list to read it.</p></div></article>
      </div>
    </main>
  </div>
</template>
