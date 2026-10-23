<script setup lang="ts">
import {t} from '../i18n'
import WorkspaceAppearance from '../components/WorkspaceAppearance.vue'
import OrganizationSettings from '../components/OrganizationSettings.vue'
import SearchPreferences from '../components/SearchPreferences.vue'
import LanguageSettings from '../components/LanguageSettings.vue'
import AccountCSVImport from '../components/AccountCSVImport.vue'
import SieveSettings from '../components/SieveSettings.vue'
import MFASettings from '../components/MFASettings.vue'
import OAuthSettings from '../components/OAuthSettings.vue'
import SettingsSearch from '../components/SettingsSearch.vue'
import CredentialSettings from '../components/CredentialSettings.vue'
import ReaderPrivacySettings from '../components/ReaderPrivacySettings.vue'
import MailProxySettings from '../components/MailProxySettings.vue'
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api/client'
import { listAccounts, listSMTPConfigs, linkSendingIdentity, deleteAccount as deleteReceivingIdentity, deleteSMTPConfig, type Account, type SMTPConfig } from '../api/accounts'
import AppHeader from '../components/AppHeader.vue'
import VaultUnlock from '../components/VaultUnlock.vue'
import AccountModal from '../components/AccountModal.vue'
import SMTPModal from '../components/SMTPModal.vue'
import AppIcon from '../components/AppIcon.vue'
import { useMailboxStore } from '../stores/mailboxes'
import { reauthenticateWithOpaque } from '../crypto/auth'
import { useSessionStore } from '../stores/session'
import { useTheme } from '../stores/theme'
import { exportUserData, importUserData } from '../composables/useImportExport'

// Global browser APIs
/* global File, Event, HTMLInputElement */
declare const Blob: typeof globalThis.Blob
declare const URL: typeof globalThis.URL
declare const document: typeof globalThis.document
declare const alert: (message: string) => void
declare const location: typeof globalThis.location

const session = useSessionStore()
const appearance=useTheme()
onMounted(()=>{void appearance.load()})
const mailbox = useMailboxStore()
const router = useRouter()
const route = useRoute()
const showModal = ref(false)
const showSMTPModal = ref(false)
const editingAccount = ref<Account | null>(null)
const editingSender = ref<SMTPConfig | null>(null)
const confirmation = ref('')
const reauthLogin = ref(session.login)
const reauthPassword = ref('')
const showDelete = ref(false)
const deleting = ref(false)
const error = ref('')
const accounts = ref<Account[]>([])
const accountsError = ref('')
const sendingIdentities = ref<SMTPConfig[]>([])
const sendingError = ref('')
const linkingAccount = ref('')
const pendingRemoval = ref<{ kind: 'receiver' | 'sender'; id: string } | null>(null)
const removing = ref(false)
const removalError = ref('')

// Import/Export
const showExport = ref(false)
const showImport = ref(false)
const exportPassword = ref('')
const exportConfirm = ref('')
const exportError = ref('')
const exportSuccess = ref('')
const importing = ref(false)
const importConfirmed = ref(false)
const exporting = ref(false)
const importPassword = ref('')
const importError = ref('')
const importFile = ref<File | null>(null)

function changeRefresh(event: { target: unknown }) {
  mailbox.setInterval(Number((event.target as { value: string }).value))
}

function openMailbox(account?: Account) {
  pendingRemoval.value = null
  editingAccount.value = account ? { ...account } : null
  showSMTPModal.value = false
  showModal.value = true
}

function openSender(config?: SMTPConfig) {
  pendingRemoval.value = null
  editingSender.value = config ? { ...config } : null
  showModal.value = false
  showSMTPModal.value = true
}

watch(showModal, open => { if (!open) editingAccount.value = null })
watch(showSMTPModal, open => { if (!open) editingSender.value = null })

function askRemoval(kind: 'receiver' | 'sender', id: string) {
  pendingRemoval.value = { kind, id }
  removalError.value = ''
}

async function removeIdentity() {
  const target = pendingRemoval.value
  if (!target || removing.value || linkingAccount.value || !session.unlocked) return
  const epoch = session.vaultEpoch
  removing.value = true
  removalError.value = ''
  try {
    if (target.kind === 'receiver') await deleteReceivingIdentity(target.id)
    else await deleteSMTPConfig(target.id)
    if (!session.unlocked || session.vaultEpoch !== epoch) return
    if (target.kind === 'receiver') accounts.value = accounts.value.filter(account => account.id !== target.id)
    else {
      sendingIdentities.value = sendingIdentities.value.filter(identity => identity.id !== target.id)
      for (const account of accounts.value) if (account.smtp_config_id === target.id) delete account.smtp_config_id
    }
    pendingRemoval.value = null
    mailbox.refresh()
  } catch (value) {
    if (!session.unlocked || session.vaultEpoch !== epoch) return
    removalError.value = value instanceof Error ? value.message : 'The identity could not be removed. Please try again.'
    // A sender removal can unlink its mailboxes before a later revision conflict.
    await Promise.all([loadAccounts(), loadSendingIdentities()])
  } finally {
    removing.value = false
  }
}

async function loadSendingIdentities() {
  sendingError.value = ''
  try {
    const saved = await listSMTPConfigs()
    if (session.unlocked) sendingIdentities.value = Object.values(saved)
  } catch (value) {
    if (session.unlocked) sendingError.value = value instanceof Error ? value.message : 'Failed to load sender addresses.'
  }
}

async function linkAccount(account: Account, event: { target: unknown }) {
  const select = event.target as { value: string }
  const sendingID = select.value
  linkingAccount.value = account.id
  accountsError.value = ''
  try {
    await linkSendingIdentity(account.id, sendingID)
    account.smtp_config_id = sendingID || undefined
  } catch (value) {
    accountsError.value = value instanceof Error ? value.message : 'Failed to link identities.'
  } finally {
    select.value = account.smtp_config_id || ''
    linkingAccount.value = ''
  }
}

async function loadAccounts() {
  accountsError.value = ''
  try {
    const saved = await listAccounts()
    if (session.unlocked) accounts.value = Object.values(saved)
  } catch (e: unknown) {
    if (session.unlocked) accountsError.value = e instanceof Error ? e.message : 'Failed to load accounts.'
  }
}

watch(() => session.authenticated && session.unlocked, (unlocked) => {
  accounts.value = []
  accountsError.value = ''
  sendingIdentities.value = []
  sendingError.value = ''
  if (unlocked) {
    void loadAccounts()
    void loadSendingIdentities()
  } else {
    showModal.value = false
    showSMTPModal.value = false
    pendingRemoval.value = null
    removalError.value = ''
  }
}, { immediate: true })

watch([() => session.authenticated && session.unlocked, () => route.query.add], ([unlocked, add]) => {
  if (!unlocked || add !== 'sender') return
  openSender()
  const query = { ...route.query }
  delete query.add
  void router.replace({ path: route.path, query, hash: route.hash })
}, { immediate: true })

async function deleteAccount() {
  deleting.value = true
  error.value = ''
  try {
    const auth = await reauthenticateWithOpaque(reauthLogin.value, reauthPassword.value)
    session.accept(auth, reauthLogin.value)
    reauthPassword.value = ''
    await api<void>('/me', { method: 'DELETE', body: JSON.stringify({ confirmation: confirmation.value }) })
    session.clearLocalSession()
    await router.push('/login')
  } catch {
    error.value = 'Reauthentication or deletion failed. Your account has not been reported as deleted.'
  } finally {
    reauthPassword.value = ''
    deleting.value = false
  }
}

function onAccountCreated() {
  showModal.value = false
  void loadAccounts()
  mailbox.refresh()
}

function onSendingIdentityCreated() {
  showSMTPModal.value = false
  void loadSendingIdentities()
  mailbox.refresh()
}

async function doExport() {
  if (!session.unlocked || exporting.value) return
  exportError.value = ''
  exportSuccess.value = ''
  if (exportPassword.value !== exportConfirm.value) {
    exportError.value = 'Passwords do not match'
    return
  }
  if (exportPassword.value.length < 8) {
    exportError.value = 'Password must be at least 8 characters'
    return
  }
  exporting.value = true
  try {
    const json = await exportUserData(exportPassword.value)
    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `mail-export-${new Date().toISOString().slice(0, 10)}.json`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    exportSuccess.value = 'Export completed successfully'
    exportPassword.value = ''
    exportConfirm.value = ''
  } catch (e) {
    exportError.value = e instanceof Error ? e.message : 'Export failed'
  } finally { exporting.value = false }
}

async function doImport() {
  if (!session.unlocked || importing.value || !importConfirmed.value) return
  importError.value = ''
  if (!importFile.value) {
    importError.value = 'Please select a file'
    return
  }
  if (importPassword.value.length < 8) {
    importError.value = 'Password must be at least 8 characters'
    return
  }
  importing.value = true
  try {
    const text = await importFile.value.text()
    await importUserData(text, importPassword.value)
    importError.value = ''
    importFile.value = null
    importPassword.value = ''
    showImport.value = false
    alert(t('Import completed. Sign in again to load the restored configuration.'))
    location.reload()
  } catch (e) {
    importError.value = e instanceof Error ? e.message : 'Import failed'
  } finally {
    importing.value = false
  }
}

function handleFileSelect(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files && input.files[0]) {
    importFile.value = input.files[0]
    importError.value = ''
  }
}
</script>

<template>
  <div class="app-shell">
    <AppHeader />
    <main v-if="session.authenticated && !session.unlocked" class="center-state">
      <VaultUnlock />
    </main>
    <main v-else class="settings">
      <div class="settings-content">
        <SettingsSearch />
        <section id="appearance"><h2>{{ t("Appearance") }}</h2><LanguageSettings /><label class="refresh-preference" for="theme-select">{{ t("Theme") }}<select id="theme-select" :value="appearance.selection.value" :disabled="!appearance.loaded.value || appearance.saving.value" @change="appearance.setTheme(($event.target as HTMLSelectElement).value)"><option v-for="theme in appearance.themes" :key="theme.id" :value="theme.id">{{ t(theme.name) }} - {{ t(theme.description) }}</option></select></label><p class="quiet">{{ appearance.saving.value ? t("Saving your theme…") : t("Saved in your encrypted profile and restored when you sign in on any device.") }}</p><p v-if="appearance.error.value" class="error" role="alert">{{ t(appearance.error.value) }} <button v-if="!appearance.loaded.value" type="button" class="text-button" @click="appearance.load()">{{ t("Try again") }}</button></p><label>{{ t("Interface accent") }}<select :value="appearance.accent?.value||'theme'" :disabled="!appearance.loaded.value||appearance.saving.value" @change="appearance.setDetails(($event.target as HTMLSelectElement).value,appearance.textScale.value)"><option value="theme">{{ t("Theme default") }}</option><option value="blue">{{ t("Blue") }}</option><option value="purple">{{ t("Purple") }}</option><option value="green">{{ t("Green") }}</option></select></label><label>{{ t("Interface text size") }}<select :value="appearance.textScale.value" :disabled="!appearance.loaded.value||appearance.saving.value" @change="appearance.setDetails(appearance.accent.value,($event.target as HTMLSelectElement).value)"><option value="normal">{{ t("Normal") }}</option><option value="large">{{ t("Large") }}</option></select></label><WorkspaceAppearance /></section>
        <section id="organization"><h2>{{t("Tags, snooze and recent history")}}</h2><OrganizationSettings /></section>
        <section id="searches"><h2>{{t("Saved searches and combined folders")}}</h2><SearchPreferences /></section>
        <section id="accounts">
          <h2 class="settings-section-title"><span class="settings-section-icon"><AppIcon name="inbox" :size="21" /></span>{{ t("Receive email") }}</h2>
          <p>{{ t("Manage the inboxes you receive mail from.") }}</p>
          <p v-if="accountsError" class="error" role="alert">{{ t(accountsError) }}</p>
          <ul v-if="accounts.length" class="account-list">
            <li v-for="account in accounts" :key="account.id">
              <div class="identity-heading"><strong>{{ account.name }}</strong><div class="identity-actions"><button type="button" class="identity-edit" :aria-label="t('Edit mailbox {0}', {'0': account.name})" :disabled="removing || !!linkingAccount" @click="openMailbox(account)"><AppIcon name="edit" :size="16" />{{ t("Edit") }}</button><button type="button" class="identity-remove" :aria-label="t('Remove mailbox {0}', {'0': account.name})" :disabled="removing || !!linkingAccount" @click="askRemoval('receiver', account.id)"><AppIcon name="trash" :size="16" />{{ t("Remove") }}</button></div></div>
              <span>{{ account.imap_username }} · {{ account.imap_host }}:{{ account.imap_port }}</span>
              <label>{{ t("Linked sender address (optional)") }} <select :value="account.smtp_config_id || ''" :disabled="!!linkingAccount || removing" @change="linkAccount(account, $event)">
                  <option value="">{{ t("None - receive only") }}</option>
                  <option v-for="identity in sendingIdentities" :key="identity.id" :value="identity.id">{{ identity.name }} - {{ identity.sender_email || identity.username }}</option>
                  <option v-if="account.smtp_config_id && !sendingIdentities.some(identity => identity.id === account.smtp_config_id)" :value="account.smtp_config_id">{{ t("Unavailable identity") }}</option>
                </select>
              </label>
              <p class="quiet">{{ t("Linking selects this sender for replies and lets you save outgoing messages in this mailbox’s Sent folder. Configure sent copies and folder mapping with Edit.") }}</p>
              <RouterLink v-if="account.smtp_config_id" :to="{ path: '/compose', query: { from: account.smtp_config_id } }">{{ t("Send email with linked address →") }}</RouterLink>
              <div v-if="pendingRemoval?.kind === 'receiver' && pendingRemoval.id === account.id" class="identity-removal" role="group" :aria-label="t('Confirm removal of mailbox {0}', {'0': account.name})">
                <p>{{ t("Remove this mailbox from your workspace? Your emails stay with your provider, and any linked sender is kept.") }}</p>
                <p v-if="removalError" class="error" role="alert">{{ t(removalError) }}</p>
                <div class="identity-removal-actions"><button type="button" class="secondary" :disabled="removing" @click="pendingRemoval = null">{{ t("Cancel") }}</button><button type="button" class="danger-button" :disabled="removing || !!linkingAccount" @click="removeIdentity">{{ removing ? t("Removing…") : t("Remove mailbox") }}</button></div>
              </div>
            </li>
          </ul>
          <button class="secondary" :disabled="removing" @click="openMailbox()">{{ t("Add mailbox") }}</button>
          <label class="refresh-preference">{{ t("Fallback refresh") }}<select :value="mailbox.refreshInterval" @change="changeRefresh"><option :value="15">{{ t("Every 15 seconds") }}</option><option :value="30">{{ t("Every 30 seconds") }}</option><option :value="60">{{ t("Every minute") }}</option><option :value="120">{{ t("Every 2 minutes") }}</option><option :value="300">{{ t("Every 5 minutes") }}</option></select></label>
          <p class="refresh-explanation">{{ t("Live updates when supported. Otherwise, check at this interval while the tab is visible.") }}</p>
        </section>
        <section id="identities">
          <h2 class="settings-section-title"><span class="settings-section-icon"><AppIcon name="send" :size="21" /></span>{{ t("Send email") }}</h2>
          <p>{{ t("Manage the addresses you send messages from.") }}</p>
          <p v-if="sendingError" class="error" role="alert">{{ t(sendingError) }}</p>
          <ul v-if="sendingIdentities.length" class="account-list">
            <li v-for="identity in sendingIdentities" :key="identity.id">
              <div class="identity-heading"><strong>{{ identity.name }}</strong><div class="identity-actions"><button type="button" class="identity-edit" :aria-label="t('Edit sender {0}', {'0': identity.name})" :disabled="removing || !!linkingAccount" @click="openSender(identity)"><AppIcon name="edit" :size="16" />{{ t("Edit") }}</button><button type="button" class="identity-remove" :aria-label="t('Remove sender {0}', {'0': identity.name})" :disabled="removing || !!linkingAccount" @click="askRemoval('sender', identity.id)"><AppIcon name="trash" :size="16" />{{ t("Remove") }}</button></div></div>
              <span>{{ identity.sender_name }} &lt;{{ identity.sender_email || identity.username }}&gt; · {{ identity.host }}:{{ identity.port }}</span>
              <div v-if="pendingRemoval?.kind === 'sender' && pendingRemoval.id === identity.id" class="identity-removal" role="group" :aria-label="t('Confirm removal of sender {0}', {'0': identity.name})">
                <p>{{ t("Remove this sender address? Linked mailboxes will be kept, with their sender link removed.") }}</p>
                <p v-if="removalError" class="error" role="alert">{{ t(removalError) }}</p>
                <div class="identity-removal-actions"><button type="button" class="secondary" :disabled="removing" @click="pendingRemoval = null">{{ t("Cancel") }}</button><button type="button" class="danger-button" :disabled="removing || !!linkingAccount" @click="removeIdentity">{{ removing ? t("Removing…") : t("Remove sender address") }}</button></div>
              </div>
            </li>
          </ul>
          <button class="secondary" :disabled="removing" @click="openSender()">{{ t("Add sender address") }}</button>
          <RouterLink v-if="sendingIdentities.length" class="compose-link" to="/compose">{{ t("Compose a message →") }}</RouterLink>
        </section>
        <MailProxySettings :key="`${session.userID}:${session.vaultEpoch}`" :accounts="accounts" :senders="sendingIdentities" />
        <section id="security">
          <h2 class="settings-section-title"><span class="settings-section-icon"><AppIcon name="shield" :size="21" /></span>{{ t("Security") }}</h2>
          <button class="secondary" @click="session.lock">{{ t("Lock vault") }}</button>
          <label class="refresh-preference">{{ t("Lock after inactivity") }}<select :value="session.idleMinutes" @change="session.setIdleMinutes(Number(($event.target as HTMLSelectElement).value))"><option :value="1">{{ t("1 minute") }}</option><option :value="5">{{ t("5 minutes") }}</option><option :value="10">{{ t("10 minutes") }}</option><option :value="15">{{ t("15 minutes") }}</option><option :value="30">{{ t("30 minutes") }}</option><option :value="60">{{ t("1 hour") }}</option></select></label>
          <label class="checkbox-option"><input type="checkbox" :checked="session.resumeOnReload" @change="session.setResumeOnReload(($event.target as HTMLInputElement).checked)">{{ t("Keep the vault unlocked when reloading this tab") }}</label>
          <p class="quiet">{{ t("Use only in a trusted browser. Your password is never saved. Lock and logout clear the encrypted tab resumption. Restored browser tabs remain subject to the inactivity deadline.") }}</p>
          <p v-if="session.resumeOnReload && !session.resumeAvailable" class="quiet">{{ t("Encrypted tab resumption is not available yet in this browser. Until it is ready, reloading locks the vault.") }}</p>
          <ReaderPrivacySettings />
        </section>
        <CredentialSettings />
        <SieveSettings :accounts="accounts" />
        <MFASettings />
        <OAuthSettings :accounts="accounts" :senders="sendingIdentities" @changed="loadAccounts();loadSendingIdentities()" />
        <section id="import-export"><AccountCSVImport />
          <h2 class="settings-section-title"><span class="settings-section-icon"><AppIcon name="arrow" :size="21" /></span>{{ t("Import / Export") }}</h2>
          <p>{{ t("Backup or restore your encrypted workspace configuration (accounts, sender identities, and preferences).") }}</p>

          <div class="import-export-grid">
            <div class="import-export-card">
              <h3>{{ t("Export") }}</h3>
              <p class="quiet">{{ t("Download an encrypted backup of your accounts, sender identities, and preferences.") }}</p>
              <button class="secondary" :disabled="!session.unlocked || showExport" @click="showExport = true">
                <AppIcon name="arrow" :size="16" /> {{ t("Export configuration") }} </button>

              <div v-if="showExport" class="import-export-modal">
                <h4>{{ t("Export configuration") }}</h4>
                <p class="quiet">{{ t("Set a password to encrypt the export file. You will need this password to import the configuration.") }}</p>
                <label>{{ t("Password") }}<input v-model="exportPassword" type="password" autocomplete="new-password" required minlength="8" @keydown.enter="doExport"></label>
                <label>{{ t("Confirm password") }}<input v-model="exportConfirm" type="password" autocomplete="new-password" required minlength="8" @keydown.enter="doExport"></label>
                <p v-if="exportError" class="error" role="alert">{{ t(exportError) }}</p>
                <p v-if="exportSuccess" class="success">{{ t(exportSuccess) }}</p>
                <div class="import-export-actions">
                  <button class="secondary" @click="showExport = false; exportPassword = ''; exportConfirm = ''; exportError = ''; exportSuccess = ''">{{ t("Cancel") }}</button>
                  <button class="primary" :disabled="exporting || !exportPassword || !exportConfirm || exportPassword !== exportConfirm" @click="doExport">{{ t("Export") }}</button>
                </div>
              </div>
            </div>

            <div class="import-export-card">
              <h3>{{ t("Import") }}</h3>
              <p class="quiet">{{ t("Restore a previously exported configuration from an encrypted backup file.") }}</p>
              <button class="secondary" :disabled="!session.unlocked || showImport" @click="showImport = true">
                <AppIcon name="arrow" :size="16" /> {{ t("Import configuration") }} </button>

              <div v-if="showImport" class="import-export-modal">
                <h4>{{ t("Import configuration") }}</h4>
                <p class="quiet">{{ t("Import replaces your saved mailboxes, senders and preferences and signs out all sessions. Export your current settings first.") }}</p>
                <label class="checkbox-option"><input v-model="importConfirmed" type="checkbox" :disabled="importing">{{ t("Replace my current configuration") }}</label>
                <label>{{ t("Export file") }}<input type="file" accept=".json" @change="handleFileSelect" :disabled="importing"></label>
                <label>{{ t("Password") }}<input v-model="importPassword" type="password" autocomplete="current-password" required minlength="8" :disabled="importing"></label>
                <p v-if="importError" class="error" role="alert">{{ t(importError) }}</p>
                <div class="import-export-actions">
                  <button class="secondary" :disabled="importing" @click="showImport = false; importConfirmed = false; importFile = null; importPassword = ''; importError = ''">{{ t("Cancel") }}</button>
                  <button class="primary" :disabled="importing || !importConfirmed || !importFile || !importPassword" @click="doImport">{{ importing ? t("Importing…") : t("Import") }}</button>
                </div>
              </div>
            </div>
          </div>
        </section>
        <section class="danger">
          <p class="eyebrow">{{ t("Danger zone") }}</p>
          <h2>{{ t("Delete my account") }}</h2>
          <p>{{ t("This permanently removes all local configuration. It does not delete mail held by providers and cannot guarantee erasure from backups or storage snapshots.") }}</p>
          <button v-if="!showDelete" class="danger-button" @click="showDelete = true">{{ t("Delete my account") }}</button>
          <form v-else @submit.prevent="deleteAccount">
            <label>{{ t("Login identifier") }}<input v-model="reauthLogin" autocomplete="username" required></label>
            <label>{{ t("Main password") }}<input v-model="reauthPassword" type="password" autocomplete="current-password" required></label>
            <label>{{ t("Type") }} <strong>{{ t("DELETE") }}</strong> {{ t("to confirm") }}<input v-model="confirmation" autocomplete="off" required></label>
            <p>{{ t("You must authenticate again with OPAQUE immediately before deletion.") }}</p>
            <p v-if="error" class="error">{{ t(error) }}</p>
            <button class="danger-button" :disabled="confirmation !== 'DELETE' || !reauthLogin || !reauthPassword || deleting">{{ deleting ? t("Reauthenticating and deleting…") : t("Permanently delete") }}</button>
            <button type="button" class="secondary" @click="showDelete = false; confirmation = ''; error = ''" :disabled="deleting">{{ t("Cancel") }}</button>
          </form>
        </section>
      </div>
    </main>
    <AccountModal v-model="showModal" :account="editingAccount" @success="onAccountCreated" />
    <SMTPModal v-model="showSMTPModal" :config="editingSender" @success="onSendingIdentityCreated" />
  </div>
</template>

