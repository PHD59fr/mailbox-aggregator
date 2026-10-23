<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { createAccount, updateAccount, type AccountInput, type Account, type FolderMapping, type FolderRole } from '../api/accounts'
import { listFolders, type FolderPage } from '../api/reading'
import { folderRoles, validateFolderMapping } from '../api/folders'
import AppIcon from './AppIcon.vue'
import ConnectionTest from './ConnectionTest.vue'

interface Props {
  modelValue: boolean
  account?: Account | null
}
interface Emits {
  (e: 'update:modelValue', value: boolean): void
  (e: 'success'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const loading = ref(false)
const error = ref('')
const serverFolders = ref<FolderPage | null>(null)
const foldersLoading = ref(false)
const foldersError = ref('')
let folderController: InstanceType<typeof globalThis.AbortController> | undefined
const mappingRoles = Object.keys(folderRoles) as FolderRole[]

const accountForm = ref<AccountInput & {folder_mapping: FolderMapping}>({
  name: '',
  imap_host: '',
  imap_port: 993,
  imap_security: 'tls',
  imap_username: '',
  password: '',
  mark_as_read: false,
  sync_folders: false,
  reply_from_recipient: false,
  save_sent: true,
  folder_mapping: {},
})
const connectionChanged = computed(() => {
  const saved = props.account; const form = accountForm.value
  return !saved || form.imap_host !== saved.imap_host || form.imap_port !== saved.imap_port || form.imap_security !== saved.imap_security || form.imap_username !== saved.imap_username || !!(form.password && form.password !== saved.password)
})
const availableFolders = computed(() => (serverFolders.value?.folders || []).filter(folder => folder.selectable && folder.name.toUpperCase() !== 'INBOX').sort((a,b) => a.name.localeCompare(b.name)))
function clearFolders() {
  folderController?.abort(); folderController = undefined
  foldersLoading.value = false; serverFolders.value = null; foldersError.value = ''
}
watch(connectionChanged, clearFolders)
onUnmounted(clearFolders)
async function loadFolders() {
  if (!props.account || connectionChanged.value || foldersLoading.value) return
  const accountID = props.account.id
  const controller = new globalThis.AbortController(); folderController = controller
  foldersLoading.value = true; foldersError.value = ''
  try {
    const page = await listFolders(accountID, controller.signal)
    if (!controller.signal.aborted && props.modelValue && props.account?.id === accountID && !connectionChanged.value) serverFolders.value = page
  } catch (value) {
    if (!controller.signal.aborted) foldersError.value = value instanceof Error ? value.message : 'Could not load server folders.'
  } finally {
    if (folderController === controller) { folderController = undefined; foldersLoading.value = false }
  }
}
function setMapping(role: FolderRole, event: {target: unknown}) {
  const value = (event.target as {value: string}).value
  if (value === 'none') accountForm.value.folder_mapping[role] = null
  else if (value.startsWith('folder:')) accountForm.value.folder_mapping[role] = value.slice(7)
  else if (!value) delete accountForm.value.folder_mapping[role]
}
function mappingValue(role: FolderRole) {
  const name = accountForm.value.folder_mapping[role]
  return name === null ? 'none' : name ? `folder:${name}` : ''
}
function assignedElsewhere(role: FolderRole, name: string) {
  return mappingRoles.some(other => other !== role && accountForm.value.folder_mapping[other] === name)
}

watch([() => props.modelValue, () => props.account], ([open]) => {
  resetForms(open ? props.account : undefined)
}, { immediate: true })

watch(() => accountForm.value.imap_security, (security) => {
  accountForm.value.imap_port = security === 'tls' ? 993 : 143
}, { flush: 'sync' })

async function submitAccount() {
  if (loading.value) return
  error.value = ''
  loading.value = true
  try {
    validateFolderMapping(accountForm.value.folder_mapping)
    if (props.account) await updateAccount(props.account.id, accountForm.value)
    else await createAccount(accountForm.value)
    emit('success')
    emit('update:modelValue', false)
    resetForms()
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to save mailbox.'
  } finally {
    loading.value = false
  }
}

function resetForms(account?: Account | null) {
  clearFolders()
  accountForm.value = account ? { name: account.name, imap_host: account.imap_host, imap_port: account.imap_port,
    imap_security: account.imap_security, imap_username: account.imap_username, password: '',mark_as_read:account.mark_as_read ?? false, sync_folders:account.sync_folders ?? false, reply_from_recipient:account.reply_from_recipient ?? false, save_sent:account.save_sent ?? true, folder_mapping:{...account.folder_mapping} }
    : { name: '', imap_host: '', imap_port: 993, imap_security: 'tls', imap_username: '', password: '',mark_as_read:false, sync_folders:false, reply_from_recipient:false, save_sent:true, folder_mapping:{} }
  // Restoring a saved security mode must not replace a custom saved port.
  accountForm.value.imap_port = account?.imap_port ?? 993
  error.value = ''
}

function close() {
  if (!loading.value) {
    resetForms()
    emit('update:modelValue', false)
  }
}
</script>

<template>
<Transition name="modal-fade">
  <div v-if="modelValue" class="modal-backdrop" @click="close">
    <div class="modal" role="dialog" aria-modal="true" aria-labelledby="account-title" @click.stop>
      <header>
        <h2 id="account-title">{{ account ? 'Edit mailbox' : 'Add mailbox' }}</h2>
        <button class="close-btn" @click="close" :disabled="loading" aria-label="Close"><AppIcon name="close" :size="17" /></button>
      </header>

      <div v-if="error" class="error-banner" role="alert">{{ error }}</div>

      <form @submit.prevent="submitAccount" class="modal-form">
        <fieldset :disabled="loading">
        <div class="field">
          <label>Mailbox name <input v-model="accountForm.name" required maxlength="128" placeholder="e.g., Personal Gmail" /></label>
        </div>
        <div class="field-row">
          <div class="field">
            <label>IMAP Host <input v-model="accountForm.imap_host" required maxlength="255" placeholder="imap.gmail.com" /></label>
          </div>
          <div class="field">
            <label>IMAP Port <input v-model.number="accountForm.imap_port" type="number" min="1" max="65535" required /></label>
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label>IMAP Security
              <select v-model="accountForm.imap_security" required>
                <option value="tls">TLS (Implicit, port 993)</option>
                <option value="starttls">STARTTLS (Explicit, port 143)</option>
              </select>
            </label>
          </div>
          <div class="field">
            <label>IMAP Username <input v-model="accountForm.imap_username" required maxlength="512" placeholder="user@gmail.com" /></label>
          </div>
        </div>
        <div class="field"><label>IMAP password<input v-model="accountForm.password" type="password" :required="!account?.password" :placeholder="account?.password ? 'Leave blank to keep current password' : ''" maxlength="4096" autocomplete="new-password" /></label></div>
        <p class="quiet">The configuration and IMAP password are encrypted in your vault.</p>
        <label class="checkbox-option"><input v-model="accountForm.mark_as_read" type="checkbox">Mark messages as read when I open them</label>
        <label class="checkbox-option"><input v-model="accountForm.save_sent" type="checkbox">Save sent messages from the linked sender in this mailbox</label>
        <p class="quiet">Link a sender in Settings to keep its sent messages here, using the Sent folder mapping below. If several mailboxes share that sender, choose one in the composer. Some providers already save sent mail; turn this off if duplicate copies appear. Sent set to None disables copying.</p>
        <label class="checkbox-option"><input v-model="accountForm.reply_from_recipient" type="checkbox">Prefer replying from the address that received the message</label>
        <p v-if="accountForm.reply_from_recipient" class="quiet">Match a sender address to the message’s To or Cc recipients. If none matches, use the first available sender. You can still change From before sending.</p>
        <label class="checkbox-option"><input v-model="accountForm.sync_folders" type="checkbox">Synchronize IMAP folders</label>
        <p v-if="accountForm.sync_folders" class="quiet">Show your server folders and refresh their counts using the fallback refresh interval. Messages load when you open a folder.</p>
        <section class="folder-mapping" aria-labelledby="folder-mapping-title">
          <h3 id="folder-mapping-title">Folder mapping</h3>
          <p class="quiet">Choose a server folder, Automatic, or None to disable a role. No server folder is created, renamed or deleted. Delete uses the selected Trash folder and is disabled when Trash is None.</p>
          <p v-if="!account" class="quiet">Save this mailbox first, then edit it to load its server folders.</p>
          <p v-else-if="connectionChanged" class="quiet">Save your connection changes before loading server folders.</p>
          <button v-else type="button" class="secondary" :disabled="foldersLoading" @click="loadFolders">{{ foldersLoading ? 'Loading folders…' : serverFolders ? 'Reload server folders' : 'Load server folders' }}</button>
          <p v-if="foldersError" class="error" role="alert">{{ foldersError }}</p>
          <p v-if="serverFolders?.truncated" class="quiet">Only the first 200 server folders are shown. Saved choices outside this list are kept.</p>
          <div class="folder-mapping-grid">
            <label v-for="role in mappingRoles" :key="role" :for="`folder-map-${role}`">{{ folderRoles[role].label }}
              <select :id="`folder-map-${role}`" :value="mappingValue(role)" :disabled="!account || connectionChanged || foldersLoading" @change="setMapping(role,$event)">
                <option value="">Automatic</option>
                <option value="none">None</option>
                <option v-if="accountForm.folder_mapping[role] && !availableFolders.some(folder => folder.name === accountForm.folder_mapping[role])" :value="mappingValue(role)">{{ accountForm.folder_mapping[role] }} ({{ !serverFolders ? 'saved' : serverFolders.truncated ? 'not in loaded list' : 'unavailable' }})</option>
                <option v-for="folder in availableFolders" :key="folder.name" :value="`folder:${folder.name}`" :disabled="assignedElsewhere(role,folder.name)">{{ folder.name }}</option>
              </select>
            </label>
          </div>
          <p v-if="!accountForm.sync_folders" class="quiet">Enable Synchronize IMAP folders above to show these folders in your mailbox.</p>
        </section>
        <ConnectionTest protocol="imap" :host="accountForm.imap_host" :port="accountForm.imap_port" :security="accountForm.imap_security" :username="accountForm.imap_username" :password="accountForm.password || account?.password || ''" />
        <div class="modal-actions">
          <button type="button" class="secondary" @click="close" :disabled="loading">Cancel</button>
          <button type="submit" class="primary" :disabled="loading">{{ loading ? 'Saving…' : account ? 'Save changes' : 'Add mailbox' }}</button>
        </div>
        </fieldset>
      </form>
    </div>
  </div>
</Transition>
</template>

