<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { apiResponse, resourceRevision } from '../api/client'
import { readCollection } from '../api/vaultCollection'
import type { Account, SMTPConfig } from '../api/accounts'
import { useSessionStore } from '../stores/session'

interface ProxyStatus {
  imap_available: boolean
  smtp_available: boolean
  hostname: string
  imap_port: number
  smtp_port: number
  imap_account_id: string
  smtp_config_id: string
  needs_update: boolean
}

const props = defineProps<{ accounts: Account[]; senders: SMTPConfig[] }>()
const session = useSessionStore()
const status = ref<ProxyStatus | null>(null)
const revision = ref('')
const imapEnabled = ref(false)
const smtpEnabled = ref(false)
const accountID = ref('')
const senderID = ref('')
const password = ref('')
const reveal = ref(false)
const busy = ref(false)
const loading = ref(false)
const error = ref('')
let generation = 0
const configured = computed(() => !!status.value?.imap_account_id || !!status.value?.smtp_config_id)
const visible = computed(() => status.value?.imap_available || status.value?.smtp_available || configured.value || error.value)

async function load() {
  const request = ++generation
  loading.value = true
  error.value = ''
  try {
    const response = await apiResponse('/proxy', { cache: 'no-store' })
    const value = await response.json() as ProxyStatus
    if (request !== generation || !session.unlocked) return
    status.value = value
    revision.value = resourceRevision(response) || ''
    accountID.value = value.imap_account_id
    senderID.value = value.smtp_config_id || '*'
    imapEnabled.value = !!value.imap_account_id && value.imap_available
    smtpEnabled.value = !!value.smtp_config_id && value.smtp_available
  } catch (value) {
    if (request === generation && session.unlocked) error.value = value instanceof Error ? value.message : 'Could not load mail client access.'
  } finally {
    if (request === generation) loading.value = false
  }
}

function generatePassword() {
  password.value = Array.from(globalThis.crypto.getRandomValues(new Uint8Array(24)), byte => byte.toString(16).padStart(2, '0')).join('')
  reveal.value = true
}

async function save(disable = false) {
  if (busy.value || !session.unlocked || !revision.value) return
  const userID = session.userID
  const epoch = session.vaultEpoch
  const active = () => session.unlocked && session.userID === userID && session.vaultEpoch === epoch
  busy.value = true
  error.value = ''
  try {
    const body: Record<string, unknown> = { password: password.value }
    if (!disable && imapEnabled.value) {
      const collection = await readCollection<Account>('accounts')
      const account = collection.records[accountID.value]
      if (!account?.password) throw new Error('Choose a mailbox with a saved IMAP password.')
      body.imap = { id: account.id, host: account.imap_host, port: account.imap_port, security: account.imap_security, username: account.imap_username, password: account.password }
      body.accounts_revision = collection.revision
    }
    if (!disable && smtpEnabled.value) {
      const collection = await readCollection<SMTPConfig>('smtp')
      const selected = senderID.value === '*' ? Object.values(collection.records) : [collection.records[senderID.value]]
      if (!selected.length || selected.some(sender => !sender?.password)) throw new Error('Choose sender addresses with saved SMTP passwords.')
      const routes = selected.map(sender => {
        if (!sender) throw new Error('Reload your sender addresses.')
        return { id: sender.id, host: sender.host, port: sender.port, security: sender.security, username: sender.username, password: sender.password, sender_email: sender.sender_email || sender.username }
      })
      if (senderID.value === '*') body.senders = routes
      else body.smtp = routes[0]
      body.smtp_revision = collection.revision
    }
    if (!active()) return
    await apiResponse('/proxy', { method: disable ? 'DELETE' : 'PUT', headers: { 'If-Match': revision.value }, ...(disable ? {} : { body: JSON.stringify(body) }) })
    if (!active()) return
    password.value = ''
    reveal.value = false
    session.showNotice(disable || (!imapEnabled.value && !smtpEnabled.value) ? 'Mail client access disabled.' : 'Mail client access saved. Use your proxy password in your mail client.')
    await load()
  } catch (value) {
    if (active()) error.value = value instanceof Error ? value.message : 'Could not save mail client access.'
  } finally {
    busy.value = false
  }
}

watch([() => props.accounts, () => props.senders], () => { if (!busy.value) void load() }, { immediate: true })
onBeforeUnmount(() => { generation++; password.value = ''; status.value = null })
</script>

<template>
  <section v-if="visible" id="mail-clients" class="mail-proxy-settings">
    <h2>Mail client access</h2>
    <p>Connect Thunderbird, Apple Mail or another mail client through this server.</p>
    <p v-if="loading" class="quiet" role="status">Loading client settings…</p>
    <form v-if="status" @submit.prevent="save()">
      <fieldset :disabled="busy || loading" class="proxy-fields">
        <p v-if="!status.imap_available && !status.smtp_available" class="quiet">Mail client access is disabled on this server.</p>
        <label v-if="status.imap_available" class="checkbox-option"><input v-model="imapEnabled" type="checkbox">Enable IMAP access</label>
        <label v-if="status.imap_available && imapEnabled">Receiving mailbox
          <select v-model="accountID" required><option value="" disabled>Choose a mailbox</option><option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }} - {{ account.imap_username }}</option></select>
        </label>
        <label v-if="status.smtp_available" class="checkbox-option"><input v-model="smtpEnabled" type="checkbox">Enable SMTP access</label>
        <label v-if="status.smtp_available && smtpEnabled">Allowed sending addresses
          <select v-model="senderID" required><option value="*">All my sending addresses</option><option v-for="sender in senders" :key="sender.id" :value="sender.id">{{ sender.name }} - {{ sender.sender_email || sender.username }}</option></select>
        </label>
        <p v-if="status.smtp_available && smtpEnabled" class="quiet">The From address selects its SMTP provider. Only the enabled addresses in your account can send; other users' addresses and unknown senders are refused.</p>
        <div v-if="imapEnabled || smtpEnabled" class="proxy-password">
          <label for="proxy-password">Dedicated proxy password<input id="proxy-password" v-model="password" :type="reveal ? 'text' : 'password'" minlength="16" maxlength="1024" autocomplete="new-password" required></label>
          <div class="identity-actions"><button type="button" class="text-button" @click="generatePassword">Generate password</button><button type="button" class="text-button" @click="reveal = !reveal">{{ reveal ? 'Hide' : 'Show' }}</button></div>
          <p class="quiet">Use a separate password of at least 16 characters for both services. Save it in your mail client before saving here; it cannot be displayed again. Enter it again, or choose a new one, whenever you save these settings.</p>
        </div>
        <p v-if="status.needs_update" class="error" role="status">Your provider settings changed. Save client access again to use the updated credentials.</p>
        <dl v-if="status.hostname" class="proxy-connection">
          <dt>Server</dt><dd>{{ status.hostname }}</dd>
          <dt>Username</dt><dd>{{ session.login || 'Your Mailbox Aggregator login' }}</dd>
          <template v-if="status.imap_available"><dt>IMAP</dt><dd>{{ status.imap_port }} / SSL/TLS</dd></template>
          <template v-if="status.smtp_available"><dt>SMTP</dt><dd>{{ status.smtp_port }} / SSL/TLS</dd></template>
          <dt>Authentication</dt><dd>Normal password / dedicated proxy password</dd>
        </dl>
        <p class="quiet">This access works while your web vault is locked or you are signed out. Only the selected provider credentials are copied, encrypted with your proxy password. Disable access below to disconnect clients. After changing a provider identity, save client access again.</p>
        <div class="identity-actions"><button v-if="status.imap_available || status.smtp_available" class="secondary" type="submit" :disabled="!revision">{{ busy ? 'Saving…' : 'Save client access' }}</button><button v-if="configured" type="button" class="danger-button" :disabled="!revision" @click="save(true)">Disable client access</button></div>
      </fieldset>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }} <button v-if="!busy" class="text-button" type="button" @click="load">Reload settings</button></p>
  </section>
</template>

<style scoped>
.proxy-fields { border:0;padding:0;margin:0;display:grid;gap:16px;min-width:0; }
.proxy-fields label:not(.checkbox-option) { display:grid;gap:8px; }
.proxy-password { display:grid;gap:8px; }
.proxy-connection { display:grid;grid-template-columns:minmax(100px,auto) minmax(0,1fr);gap:8px 20px;font-size:.85rem; }
.proxy-connection dt { color:var(--muted); }
.proxy-connection dd { margin:0;overflow-wrap:anywhere; }
</style>
