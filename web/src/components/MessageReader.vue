<script setup lang="ts">
import { t, formatDate, formatNumber } from '../i18n'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { documentURL, messageSource, type Message, type Attachment, type MessageRef } from '../api/reading'
import { useReaderPreferences } from '../stores/readerPreferences'
import AppIcon from './AppIcon.vue'
import {messageHints} from '../api/messageHints'
import RuleFromMessage from './RuleFromMessage.vue'
import MessageAddresses from './MessageAddresses.vue'

const props = defineProps<{ message: Message | null; loading: boolean; error: string; downloading: string; reference?: MessageRef | null; marking?: boolean; deleting?: boolean; inTrash?: boolean; trashDisabled?: boolean }>()
defineEmits<{ close: []; retry: []; download: [attachment: Attachment]; action: [action: 'reply' | 'reply-all' | 'forward' | 'forward-attachment' | 'redirect'|'draft']; 'toggle-seen': []; trash: [] }>()
const confirmTrash=ref(false)
const moreActions=ref<InstanceType<typeof globalThis.HTMLDetailsElement>|null>(null)
const moreOpen=ref(false)
function closeMoreActions(restoreFocus=false){
  if(moreActions.value)moreActions.value.open=false
  moreOpen.value=false
  if(restoreFocus)moreActions.value?.querySelector('summary')?.focus()
}
function outsideActions(event:InstanceType<typeof globalThis.Event>){if(event.target instanceof globalThis.Node&&!moreActions.value?.contains(event.target))closeMoreActions()}
function leaveActions(event:InstanceType<typeof globalThis.FocusEvent>){if(event.relatedTarget instanceof globalThis.Node&&!moreActions.value?.contains(event.relatedTarget))closeMoreActions()}
function blurActions(){closeMoreActions()}
onMounted(()=>{globalThis.document.addEventListener('pointerdown',outsideActions);globalThis.addEventListener('blur',blurActions)})
onUnmounted(()=>{globalThis.document.removeEventListener('pointerdown',outsideActions);globalThis.removeEventListener('blur',blurActions)})
watch([()=>props.inTrash,()=>props.trashDisabled],()=>{if(props.inTrash || props.trashDisabled)confirmTrash.value=false})
const preferences = useReaderPreferences()
onMounted(() => { void preferences.load() })
const once = ref(false); const blocked = ref(false); const privacyOpen = ref(false)
const sender = computed(() => props.message?.from[0]?.email || '')
const senderDomain = computed(() => sender.value.trim().toLowerCase().split('@')[1] || '')
const trustedSender = computed(() => preferences.data.addresses.includes(sender.value.trim().toLowerCase()))
const trustedDomain = computed(() => preferences.data.domains.includes(senderDomain.value))
const external = computed(() => !blocked.value && (once.value || preferences.allowed(sender.value)))
const frameURL = computed(() => props.reference ? documentURL(props.reference,external.value) : undefined)
const sourceOpen = ref(false); const sourceLoading = ref(false); const sourceError = ref(''); const sourceText = ref('')
let sourceBlob: Awaited<ReturnType<typeof messageSource>> | null = null; let sourceController = new globalThis.AbortController()
function clearSource() { sourceController.abort(); sourceController = new globalThis.AbortController(); sourceBlob = null; sourceText.value = ''; sourceOpen.value = false; sourceLoading.value = false; sourceError.value = '' }
watch(() => `${props.reference?.accountID}:${props.reference?.folder}:${props.reference?.validity}:${props.message?.uid}`, () => { once.value = false; blocked.value = false; privacyOpen.value = false; confirmTrash.value=false; closeMoreActions();clearSource() })
onUnmounted(clearSource)
async function trust(domain: boolean) { try { await preferences.trust(sender.value,domain); blocked.value = false } catch { /* Store reports the error. */ } }
async function toggleSenderTrust() {
  if (!trustedSender.value) { await trust(false); return }
  try { await preferences.remove(sender.value.trim().toLowerCase(),false); once.value = false; blocked.value = false } catch { /* Store reports the error. */ }
}
async function toggleDomainTrust() {
  if (!trustedDomain.value) { await trust(true); return }
  try { await preferences.remove(senderDomain.value,true); once.value = false; blocked.value = false } catch { /* Store reports the error. */ }
}
async function showSource() {
  if (!props.reference) return
  clearSource(); sourceOpen.value = true; sourceLoading.value = true
  const signal = sourceController.signal
  try { const blob = await messageSource(props.reference,signal); const text = await blob.text(); if (!signal.aborted) { sourceBlob = blob; sourceText.value = text } }
  catch (error) { if (!signal.aborted) sourceError.value = error instanceof Error ? error.message : 'Could not load message source.' }
  finally { if (!signal.aborted) sourceLoading.value = false }
}
function saveSource() {
  if (!sourceBlob) return
  const url = globalThis.URL.createObjectURL(sourceBlob); const anchor = globalThis.document.createElement('a')
  anchor.href = url; anchor.download = `message-${props.message?.uid || 'original'}.eml`; anchor.click(); globalThis.URL.revokeObjectURL(url)
}
const plain = ref(false)
watch(() => props.message?.uid, () => { plain.value = false })
const date = computed(() => props.message ? formatDate(props.message.date) : '')
const document = computed(() => {
  // Opaque-origin sandbox + no network, scripts, forms, frames or navigation.
  // HTML is additionally allowlist-sanitized by the API before reaching us.
  const origin = globalThis.location.origin
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${origin}; img-src 'none'; base-uri 'none'; form-action 'none'"><link rel="stylesheet" href="${origin}/reader.css"></head><body>${props.message?.html || ''}</body></html>`
})
</script>
<template>
  <div class="reader-toolbar"><button type="button" class="reader-back secondary" @click="$emit('close')"><AppIcon name="arrow" :size="15" />{{ t("Back to messages") }}</button><button type="button" class="privacy-toggle text-button" :aria-expanded="privacyOpen" @click="privacyOpen = !privacyOpen"><AppIcon name="shield" :size="14" />{{ external ? t("External images allowed") : t("External images blocked") }}</button></div>
  <div v-if="privacyOpen" class="reader-privacy-panel"><p>{{ t("External images can reveal your IP address and that you opened a message. Sender addresses can be spoofed.") }}</p><div class="reader-actions"><button v-if="!external" type="button" class="secondary" @click="once = true; blocked = false">{{ t("Load images once") }}</button><button v-else type="button" class="secondary" @click="blocked = true; once = false">{{ t("Block images") }}</button><button type="button" class="secondary" :disabled="!sender || !preferences.loaded || preferences.saving" @click="trust(false)">{{ t("Always allow") }} {{ sender || t('this sender') }}</button><button type="button" class="secondary" :disabled="!senderDomain || !preferences.loaded || preferences.saving" @click="trust(true)">{{ t("Always allow *@") }}{{ senderDomain || t('domain') }}</button></div><p v-if="preferences.error" class="error" role="alert">{{ t(preferences.error) }}</p></div>
  <p v-if="error" class="reader-error error" role="alert">{{ t(error) }} <button v-if="!message && !loading" type="button" class="text-button" @click="$emit('retry')">{{ t("Try again") }}</button></p>
  <div v-if="loading" class="empty-mail" role="status"><span class="empty-icon"><AppIcon name="mail" :size="28" /></span><p>{{ t("Opening message…") }}</p></div>
  <template v-else-if="message">
    <div class="reader-actions message-actions">
      <button v-if="message.draft" type="button" class="secondary" :disabled="message.truncated" @click="$emit('action','draft')">{{ t("Edit draft") }}</button>
      <button type="button" class="secondary" @click="$emit('action', 'reply')"><AppIcon name="reply" :size="15" />{{ t("Reply") }}</button>
      <button type="button" class="secondary" @click="$emit('action', 'reply-all')"><AppIcon name="reply-all" :size="15" />{{ t("Reply all") }}</button>
      <button type="button" class="secondary" @click="$emit('action', 'forward')"><AppIcon name="forward" :size="15" />{{ t("Forward") }}</button>
      <button type="button" class="secondary" :disabled="marking" @click="$emit('toggle-seen')"><AppIcon :name="marking ? 'refresh' : message.seen ? 'mail' : 'check'" :size="15" />{{ marking ? t("Updating…") : message.seen ? t("Mark as unread") : t("Mark as read") }}</button>
      <button v-if="!inTrash" type="button" class="text-button" :disabled="deleting || trashDisabled" @click="confirmTrash = true"><AppIcon name="trash" :size="15" />{{ deleting ? t("Moving…") : t("Delete") }}</button>
      <details ref="moreActions" class="message-more-actions" @toggle="moreOpen=($event.target as HTMLDetailsElement).open" @keydown.esc.stop.prevent="closeMoreActions(true)" @focusout="leaveActions">
        <summary :aria-label="t('More message actions')" :title="t('More message actions')"><span aria-hidden="true">⋯</span></summary>
        <div class="message-more-menu" role="group" :aria-label="t('More message actions')" @click="closeMoreActions()">
          <button type="button" @click="$emit('action','forward-attachment')"><AppIcon name="paperclip" :size="15" />{{t("Forward as attachment")}}</button>
          <button type="button" @click="$emit('action','redirect')"><AppIcon name="redirect" :size="15" />{{t("Redirect")}}</button>
          <RuleFromMessage v-if="moreOpen&&reference" :message="message" :reference="reference" />
          <button type="button" @click="showSource"><AppIcon name="code" :size="15" />{{t("View source")}}</button>
        </div>
      </details>
    </div>
    <p v-if="trashDisabled" class="quiet">{{ t("Delete is unavailable: Trash is set to None in this mailbox’s folder mapping.") }}</p>
    <div v-if="confirmTrash && !inTrash && !trashDisabled" class="trash-confirm" role="group" :aria-label="t('Confirm move to Trash')"><p>{{ t("Move this message to your server’s Trash? It will not be permanently deleted.") }}</p><div class="reader-actions"><button type="button" class="secondary" :disabled="deleting" @click="confirmTrash = false">{{ t("Cancel") }}</button><button type="button" class="danger-button" :disabled="deleting" @click="$emit('trash')">{{ deleting ? t("Moving…") : t("Move to Trash") }}</button></div></div>
    <details v-if="messageHints(message).length" class="reader-warning"><summary>{{ t("Sender details to check") }}</summary><ul><li v-for="hint in messageHints(message)" :key="hint">{{t(hint)}}</li></ul><p>{{ t("These hints are not an authentication verdict. Their absence does not establish trust.") }}</p></details>
    <header class="message-heading"><h2 dir="auto">{{ message.subject || t('(No subject)') }}</h2><time :datetime="message.date">{{ date }}</time><dl><dt>{{ t("From") }}</dt><dd dir="auto"><MessageAddresses :addresses="message.from" :fallback="t('Unknown sender')" /><span v-if="sender" class="sender-trust-actions"><button type="button" class="sender-trust text-button" :disabled="!preferences.loaded || preferences.saving" :aria-pressed="trustedSender" :aria-label="`${trustedSender ? t('Remove image permission for') : t('Always allow images from')} ${sender}`" @click="toggleSenderTrust"><AppIcon :name="trustedSender ? 'check' : 'shield'" :size="14" />{{ trustedSender ? t("Trusted sender") : t("Trust sender") }}</button><button v-if="senderDomain" type="button" class="domain-trust text-button" :disabled="!preferences.loaded || preferences.saving" :aria-pressed="trustedDomain" :aria-label="`${trustedDomain ? t('Remove image permission for') : t('Always allow images from')} *@${senderDomain}`" @click="toggleDomainTrust"><AppIcon :name="trustedDomain ? 'check' : 'shield'" :size="14" />{{ trustedDomain ? t("Trusted domain") : t("Trust domain") }} *@{{ senderDomain }}</button></span></dd><template v-if="message.reply_to?.length"><dt>{{t("Reply-To")}}</dt><dd dir="auto"><MessageAddresses :addresses="message.reply_to" /></dd></template><dt>{{ t("To") }}</dt><dd dir="auto"><MessageAddresses :addresses="message.to" :fallback="t('Undisclosed recipients')" /></dd><template v-if="message.cc.length"><dt>{{ t("Cc") }}</dt><dd dir="auto"><MessageAddresses :addresses="message.cc" /></dd></template></dl><p v-if="sender" class="sender-trust-note quiet">{{ t("Trust allows external images. Sender addresses can be spoofed.") }}<template v-if="trustedDomain"> {{ t("Images are allowed for all addresses matching *@") }}{{ senderDomain }}.</template></p><p v-if="preferences.error && !privacyOpen" class="error" role="alert">{{ t(preferences.error) }}</p></header>
    <div v-if="message.html && message.text" class="reader-view-toggle"><button type="button" :class="{ active: !plain }" :aria-pressed="!plain" @click="plain = false">{{ t("Formatted") }}</button><button type="button" :class="{ active: plain }" :aria-pressed="plain" @click="plain = true">{{ t("Plain text") }}</button></div>
    <p v-if="message.truncated" class="reader-warning" role="status">{{ t("Some message content exceeds the safe display limit and was not loaded.") }}</p>
    <iframe v-if="message.html && !plain" class="message-document" :title="t('Email content')" sandbox="allow-popups allow-popups-to-escape-sandbox" referrerpolicy="no-referrer" :src="frameURL" :srcdoc="frameURL ? undefined : document" />
    <pre v-else-if="message.text" class="message-plaintext" dir="auto">{{ message.text }}</pre>
    <p v-else class="reader-warning">{{ t("This message has no displayable text content.") }}</p>
    <div v-if="message.attachments.length" class="reader-attachments"><p class="section-label">{{ t("Attachments ·") }} {{ message.attachments.length }}</p><button v-for="attachment in message.attachments" :key="attachment.part" type="button" :disabled="!!downloading" @click="$emit('download', attachment)"><AppIcon name="paperclip" :size="17" /><span>{{ attachment.filename }}<small>{{ formatNumber(Math.max(1, Math.ceil(attachment.size / 1024))) }} {{ t("KB") }}</small></span><AppIcon :name="downloading === attachment.part ? 'refresh' : 'arrow'" :size="15" /></button><small>{{ t("Downloads up to 10 MB. Files stay in memory until you save them.") }}</small></div>
  </template>
  <Teleport to="body"><div v-if="sourceOpen" class="source-backdrop" @click.self="clearSource"><section class="source-dialog" role="dialog" aria-modal="true" aria-labelledby="source-title"><header><h2 id="source-title">{{ t("Original message source") }}</h2><button type="button" class="icon-button" :aria-label="t('Close message source')" @click="clearSource"><AppIcon name="close" :size="18" /></button></header><p class="quiet">{{ t("Complete headers and MIME content · up to 20 MB. Displayed as text, never executed.") }}</p><p v-if="sourceLoading" role="status">{{ t("Loading source…") }}</p><p v-if="sourceError" class="error" role="alert">{{ t(sourceError) }}</p><pre v-if="sourceText" class="message-source" dir="ltr">{{ sourceText }}</pre><button v-if="sourceText" type="button" class="secondary" @click="saveSource">{{ t("Download original .eml") }}</button></section></div></Teleport>
</template>
