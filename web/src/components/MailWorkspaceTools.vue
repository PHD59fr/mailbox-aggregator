<script setup lang="ts">
import {t} from '../i18n'
import {useMailOrganization} from '../stores/organization'
import {computed} from 'vue'
import {useSearchAlerts} from '../composables/useSearchAlerts'
import {emptySearchHistory,readSearchHistory,rememberSearch,type SearchHistory} from '../api/searchHistory'
import { onUnmounted, ref, watch } from 'vue'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import { combinedViews, type CombinedView, type MailSource } from '../api/mailSources'
import { listSavedSearches, saveSearch, deleteSearch, readPreference, type SavedSearch } from '../api/preferences'
import type { FolderPage } from '../api/reading'

const props = defineProps<{ accountID: string; folder: string; folders: Record<string, FolderPage> }>()
const emit = defineEmits<{ view: [view: CombinedView]; mailbox: [accountID: string, folder: string] }>()
const mailbox = useMailboxStore(), session = useSessionStore()
const organization=useMailOrganization()
const tags=computed(()=>[...new Set(Object.values(organization.data.entries).flatMap(mark=>mark.tags))].sort())
const searches = ref<SavedSearch[]>([]), selectedSearch = ref(''), name = ref(''), error = ref(''), busy = ref(false)
const alertEnabled=ref(false),history=ref<SearchHistory>(emptySearchHistory())
const alerts=useSearchAlerts(searches,()=>props.folders)
interface CombinedFolders { id: 'combined-folders'; sources: MailSource[] }
let generation = 0
watch(() => [session.unlocked,session.vaultEpoch], async () => {
  const request = ++generation
  history.value=emptySearchHistory();alertEnabled.value=false;searches.value = []; selectedSearch.value = ''; name.value = ''; error.value = ''; mailbox.selectedFolders = []
  if (!session.unlocked) return
  try {
    const [saved, combined,past] = await Promise.all([listSavedSearches(), readPreference<CombinedFolders>('combined-folders', () => ({id:'combined-folders', sources:[]})),readSearchHistory()])
    if (request !== generation || !session.unlocked) return
    searches.value = saved;history.value=past
    mailbox.selectedFolders = Array.isArray(combined.sources) ? combined.sources.filter(source => source && typeof source.accountID === 'string' && typeof source.folder === 'string' && source.folder.length <= 1024).slice(0,100) : []
  } catch (value) { if (request === generation) error.value = value instanceof Error ? value.message : 'Could not load saved views.' }
}, {immediate: true})
onUnmounted(()=>{generation++;searches.value=[];name.value=''})
async function action(run: () => Promise<void>) {
  if (busy.value || !session.unlocked) return
  const request = generation
  busy.value = true; error.value = ''
  try { await run() } catch(value) { if (request === generation) error.value = value instanceof Error ? value.message : 'Could not save this view.' }
  finally { busy.value = false }
}
async function save() {
  const request = generation
  const value: SavedSearch = {id: selectedSearch.value, name: name.value, accountID: props.accountID, folder: props.folder, view: mailbox.view, scope: mailbox.searchScope,alert:alertEnabled.value,tagFilter:mailbox.tagFilter, sources: [...mailbox.selectedFolders], filters: {
    search: mailbox.search.trim(), expression: mailbox.advancedSearch.trim(), seen: mailbox.readFilter, from: mailbox.senderFilter.trim(), to: mailbox.recipientFilter.trim(), subject: mailbox.subjectFilter.trim(), since: mailbox.sinceFilter, until: mailbox.untilFilter,
    ...(mailbox.starredFilter ? {flagged:'true'} : {}),
  }}
  await action(async () => { const result = await saveSearch(value); if (request === generation && session.unlocked) searches.value = result })
}
function apply() {
  const search = searches.value.find(item => item.id === selectedSearch.value)
  if (!search) { name.value = ''; return }
  if (search.accountID && !mailbox.accounts.some(account => account.id === search.accountID)) { error.value = 'This search refers to a removed account. Choose another account and save it again.'; return }
  const view = Object.prototype.hasOwnProperty.call(combinedViews, search.view || '') ? search.view as CombinedView : 'inbox'
  if (search.accountID) emit('mailbox', search.accountID, search.folder || 'INBOX')
  else emit('view', view)
  mailbox.searchScope = search.scope === 'all' ? 'all' : 'current'
  if (Array.isArray(search.sources)) mailbox.selectedFolders = search.sources.slice(0,100)
  const filters = search.filters
  mailbox.search = filters.search || ''; mailbox.advancedSearch = filters.expression || '';  mailbox.readFilter = filters.seen || ''; mailbox.senderFilter = filters.from || ''
  mailbox.recipientFilter = filters.to || ''; mailbox.subjectFilter = filters.subject || ''; mailbox.sinceFilter = filters.since || ''; mailbox.untilFilter = filters.until || ''; mailbox.starredFilter = filters.flagged === 'true'
  mailbox.tagFilter=search.tagFilter||'';alertEnabled.value=search.alert===true;name.value = search.name; error.value = ''
}
async function remove() {
  const id = selectedSearch.value, request = generation
  await action(async () => {const result = await deleteSearch(id); if(request === generation && session.unlocked) {searches.value = result; selectedSearch.value = ''; name.value = ''}})
}

function currentSearch():SavedSearch{return {id:'',name:mailbox.search||mailbox.advancedSearch||mailbox.subjectFilter||'Filtered search',accountID:props.accountID,folder:props.folder,view:mailbox.view,scope:mailbox.searchScope,sources:[...mailbox.selectedFolders],tagFilter:mailbox.tagFilter,filters:{search:mailbox.search,expression:mailbox.advancedSearch,seen:mailbox.readFilter,from:mailbox.senderFilter,to:mailbox.recipientFilter,subject:mailbox.subjectFilter,since:mailbox.sinceFilter,until:mailbox.untilFilter,...(mailbox.starredFilter?{flagged:'true'}:{})}}}
let historyTimer:ReturnType<typeof globalThis.setTimeout>|undefined
watch(()=>JSON.stringify(currentSearch()),()=>{globalThis.clearTimeout(historyTimer);if(!history.value.remember||!session.unlocked)return;const g=generation;historyTimer=globalThis.setTimeout(()=>{if(g===generation&&session.unlocked)void rememberSearch(currentSearch()).then(async()=>{const past=await readSearchHistory();if(g===generation&&session.unlocked)history.value=past}).catch(()=>{})},2000)})
onUnmounted(()=>globalThis.clearTimeout(historyTimer))

function applyHistory(index:number){const search=history.value.searches[index];if(!search)return;const existing=searches.value;searches.value=[...existing,search];selectedSearch.value=search.id;apply();searches.value=existing;selectedSearch.value='';alertEnabled.value=false}
</script>
<template>
  <div class="mail-search-tools">
    <fieldset v-show="mailbox.filtersOpen" :disabled="busy" class="mail-search-options">
      <label>{{ t("Search scope") }}<select v-model="mailbox.searchScope"><option value="current">{{ t("Current view") }}</option><option value="all">{{ t("All accounts and synchronized folders") }}</option></select></label>
      <label><input v-model="mailbox.starredFilter" type="checkbox"> {{ t("Starred messages only") }}</label>
      <label>{{ t("Advanced query") }}<input v-model="mailbox.advancedSearch" maxlength="2048" :placeholder="t('from:alice OR (subject:invoice AND NOT is:read)')"></label>
      <p class="quiet">{{ t("Use AND, OR, NOT and parentheses. Fields: text:, from:, to:, cc:, subject:, is:read, is:unread, is:starred. Use double quotes for phrases.") }}</p>
      <label>{{ t("Saved search") }}<select v-model="selectedSearch" @change="apply"><option value="">{{ t("New saved search") }}</option><option v-for="search in searches" :key="search.id" :value="search.id">{{ search.name }}</option></select></label>
      <label><input v-model="alertEnabled" type="checkbox"> {{ t("Check this saved search for new results while this mail view is open") }}</label>
      <p class="quiet">{{ t("At most three searches, ten folders each, every two minutes while visible and unlocked. The first check sets a baseline. Only loaded result windows are compared; no notification is promised while closed.") }}</p>
      <label>{{ t("Search name") }}<input v-model="name" maxlength="128" :placeholder="t('For example: unread invoices')"></label>
      <button type="button" class="secondary" :disabled="!name.trim()" @click="save">{{selectedSearch ? t("Update saved search") : t("Save current search")}}</button>
      <button v-if="selectedSearch" type="button" class="text-button" @click="remove">{{ t("Delete saved search") }}</button>
      <label v-if="mailbox.view==='tagged'">{{t("Tag in the Tagged view")}}<select v-model="mailbox.tagFilter"><option value="">{{t("All tags")}}</option><option v-for="tag in tags" :key="tag" :value="tag">{{tag}}</option></select></label>
      <details v-if="history.searches.length"><summary>{{ t("Search history (optional)") }}</summary><ul><li v-for="(search,index) in history.searches" :key="search.id"><button type="button" class="text-button" @click="applyHistory(index)">{{search.name}}</button></li></ul></details>
    </fieldset>
    <div v-if="alerts.notices.value.length" role="status"><p v-for="(notice,index) in alerts.notices.value" :key="index">{{t(notice)}}</p><button type="button" class="text-button" @click="alerts.notices.value=[]">{{ t("Dismiss alerts") }}</button></div><p v-if="alerts.error.value" class="quiet">{{t(alerts.error.value)}}</p>
    <p v-if="error" class="error" role="alert">{{t(error)}}</p>
  </div>
</template>
<style scoped>
.mail-search-options { display:grid;gap:12px;margin-block-end:16px;padding:16px;border:1px solid var(--line);border-radius:12px; }
.mail-search-options p { margin:0; }
</style>
