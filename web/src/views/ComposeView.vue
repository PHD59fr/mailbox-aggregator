<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AppHeader from '../components/AppHeader.vue'
import VaultUnlock from '../components/VaultUnlock.vue'
import AppIcon from '../components/AppIcon.vue'
import RichTextEditor from '../components/RichTextEditor.vue'
import { listAccounts, listSMTPConfigs, type Account, type SMTPConfig } from '../api/accounts'
import { saveRemoteDraft,deleteRemoteDraft } from '../api/drafts'
import { APIError } from '../api/client'
import { listContacts,type Contact } from '../api/preferences'
import { withSignature } from '../api/signatures'
import { sendMessage, type SendResult } from '../api/messages'
import { encodeAttachments, validateAttachments, type AttachmentFile, type AttachmentInput } from '../api/attachments'
import { useSessionStore } from '../stores/session'
import { useMailActions, type MailAction } from '../stores/mailActions'
import { actionDraft, recipientReplySender } from '../api/mailActions'
import { attachmentBlob, type MessageRef } from '../api/reading'
import { redirectMessage } from '../api/redirect'
import { useMailboxStore } from '../stores/mailboxes'
import { createComposeDraftSession, disposeComposeDraftSession, draftAttachmentFile, type ComposeDraftSession, type DraftInput, type DraftStatus } from '../crypto/composeDraft'

const props = defineProps<{ smtpConfigID?: string }>()
const session = useSessionStore()
const router = useRouter()
const mailActions = useMailActions()
const remoteDraft=ref<MessageRef|null>(null);const remoteAccountID=ref('');const remoteSaving=ref(false);const remoteUnconfirmed=ref(false);const remoteStatus=ref('');const remoteDeleteConfirmed=ref(false)
const action = ref<MailAction | null>(null)
const original = ref<MessageRef | null>(null)
const inReplyTo = ref<string | undefined>()
const actionController = new globalThis.AbortController()
// A redirect already submitted to the server cannot be recalled. Do not abort
// that request just because the composer was unmounted by a lock or a route
// change; its outcome must still be reported.
const sendController = new globalThis.AbortController()
const forwardIncomplete = ref(false)
const title = computed(() => action.value==='draft'?'Edit draft':action.value === 'reply' ? 'Reply' : action.value === 'reply-all' ? 'Reply all' : action.value === 'forward' ? 'Forward message' : action.value === 'redirect' ? 'Redirect message' : 'New message')
const contacts=ref<Contact[]>([])
const configs = ref<SMTPConfig[]>([])
const accounts = ref<Account[]>([])
const selectedSentID=ref('')
const sentCopyError=ref('')
const selectedID = ref('')
const linkedMailboxes=computed(()=>accounts.value.filter(account=>account.smtp_config_id===selectedID.value && account.save_sent!==false && account.folder_mapping?.sent!==null))
watch([selectedID,linkedMailboxes],()=>{
  selectedSentID.value=(linkedMailboxes.value.find(account=>account.id===original.value?.accountID) || linkedMailboxes.value.find(account=>account.is_default) || linkedMailboxes.value[0])?.id || ''
})
const selected = computed(() => configs.value.find(config => config.id === selectedID.value))
const to = ref('')
const cc = ref('')
const showCc = ref(false)
const bcc = ref('')
const showBcc = ref(false)
const attachments = ref<AttachmentFile[]>([])
const fileInput = ref<{ click(): void } | null>(null)
const subject = ref('')
const signatureID=ref('');const signatureText=ref('')
function chooseSignature(){signatureText.value=selected.value?.signatures?.find(item=>item.id===signatureID.value)?.text||''}
const text = ref('')
const html = ref('')
const bodyValid=ref(true)
const password = ref('')
const loading = ref(false)
const sending = ref(false)
const pendingDraftID = ref('')
const submissionPending = computed(() => session.pendingDraftIDs.includes(pendingDraftID.value))
const error = ref('')
const draftStatus = ref<DraftStatus>('idle')
const draftError = ref('')
const draftRestored = ref(false)
const draftDirty = ref(false)
let draftSession: ComposeDraftSession | null = null
let draftInitialized = false
let draftAttachmentIDs = new WeakMap<AttachmentFile, string>()
let draftTimer: ReturnType<typeof globalThis.setTimeout> | undefined
let draftRevision = 0
let applyingDraft = false
let loadVersion = 0
let unmounted = false

function composeHasContent() {
  return attachments.value.length > 0 || Boolean(to.value.trim() || cc.value.trim() || bcc.value.trim() || subject.value.trim() || text.value.trim() || html.value.trim())
}

function draftInput(): DraftInput {
  return {
    action: action.value,
    original: original.value,
    inReplyTo: inReplyTo.value,
    smtpConfigID: selectedID.value || undefined,
    to: to.value, cc: cc.value, bcc: bcc.value, subject: subject.value,
    text: text.value, html: html.value, showCc: showCc.value, showBcc: showBcc.value,
    remoteDraft:remoteDraft.value,remoteUnconfirmed:remoteUnconfirmed.value,selectedSentID: selectedSentID.value,signatureID:signatureID.value,signatureText:signatureText.value,
  }
}

function ensureDraftSession(): ComposeDraftSession | null {
  if (!session.unlocked || !session.vmk) return null
  if (draftSession) return draftSession
  const vmk = session.vmk
  const userID = session.userID
  const epoch = session.vaultEpoch
  draftSession = createComposeDraftSession(
    vmk,
    userID,
    () => session.unlocked && session.vmk === vmk && session.vaultEpoch === epoch && session.userID === userID,
    draftInput,
  )
  pendingDraftID.value = draftSession.id
  return draftSession
}

function clearDraftTimer() {
  if (draftTimer) { globalThis.clearTimeout(draftTimer); draftTimer = undefined }
}

function resetDraftSession() {
  clearDraftTimer()
  if (draftSession) { disposeComposeDraftSession(draftSession); draftSession = null }
  pendingDraftID.value = ''
  draftInitialized = false
  draftStatus.value = 'idle'
  draftError.value = ''
  draftRestored.value = false
  draftDirty.value = false
}

function clearComposeState() {
  action.value = null;remoteDraft.value=null;remoteAccountID.value='';remoteUnconfirmed.value=false;remoteStatus.value='';remoteDeleteConfirmed.value=false
  original.value = null
  inReplyTo.value = undefined
  to.value = ''; cc.value = ''; bcc.value = ''
  showCc.value = false; showBcc.value = false
  subject.value = ''; text.value = ''; html.value = '';signatureID.value='';signatureText.value=''
  attachments.value = []
  draftAttachmentIDs = new WeakMap()
  forwardIncomplete.value = false
}

async function saveDraft(): Promise<void> {
  clearDraftTimer()
  const draft = draftSession
  if (!draft) return
  if (!draft.enabled) { draftDirty.value = composeHasContent(); return }
  const revision = ++draftRevision
  draftStatus.value = 'saving'
  draftError.value = ''
  try {
    await draft.save()
    if (revision !== draftRevision) return
    draftDirty.value = false
    draftStatus.value = draft.status
    draftError.value = draft.error
  } catch (value) {
    if (revision !== draftRevision) return
    draftStatus.value = 'error'
    draftError.value = value instanceof Error ? value.message : 'The draft could not be saved.'
    draftDirty.value = composeHasContent()
  }
}

async function flushDraft(): Promise<void> {
  clearDraftTimer()
  const draft = draftSession
  if (!draft) return
  if (!draft.enabled) { draftDirty.value = composeHasContent(); return }
  const revision = ++draftRevision
  draftStatus.value = 'saving'
  draftError.value = ''
  try {
    await draft.flush()
    if (revision !== draftRevision) return
    draftDirty.value = false
    draftStatus.value = draft.status
    draftError.value = draft.error
  } catch (value) {
    if (revision !== draftRevision) return
    draftStatus.value = 'error'
    draftError.value = value instanceof Error ? value.message : 'The draft could not be saved.'
    draftDirty.value = composeHasContent()
  }
}

function queueDraftSave() {
  if (!draftSession || !session.unlocked || applyingDraft) return
  draftDirty.value = composeHasContent()
  clearDraftTimer()
  draftTimer = globalThis.setTimeout(() => { draftTimer = undefined; void saveDraft() }, 600)
}

async function persistAddedFiles(files: readonly AttachmentFile[]) {
  const draft = draftSession
  if (!files.length) return
  if (!draft || !draft.enabled) { draftDirty.value = composeHasContent(); return }
  draftDirty.value = true
  draftStatus.value = 'saving'
  draftError.value = ''
  for (const file of files) {
    try {
      const stored = await draft.addAttachment(file)
      if (attachments.value.includes(file)) draftAttachmentIDs.set(file, stored.id)
      else await draft.removeAttachment(stored.id)
    } catch (value) {
      draftStatus.value = 'error'
      draftError.value = value instanceof Error ? value.message : 'The attachment could not be saved in the draft.'
    }
  }
  if (draftStatus.value !== 'error') {
    draftStatus.value = 'saved'
    draftDirty.value = false
  }
}

async function removeAttachment(index: number) {
  const [file] = attachments.value.splice(index, 1)
  if (!file) return
  draftDirty.value = true
  const id = draftAttachmentIDs.get(file)
  const draft = draftSession
  if (!id || !draft) { draftDirty.value = composeHasContent(); return }
  draftStatus.value = 'saving'
  try {
    await draft.removeAttachment(id)
    draftStatus.value = draft.status === 'error' ? 'error' : 'saved'
    draftError.value = draft.error
    draftDirty.value = false
  } catch (value) {
    draftStatus.value = 'error'
    draftError.value = value instanceof Error ? value.message : 'The attachment could not be removed from the saved draft.'
  }
}

async function restoreDraft(version: number) {
  if (draftInitialized) return
  const draft = ensureDraftSession()
  if (!draft) return
  draftInitialized = true
  if (session.sentDraftID === draft.id) { await discardSentDraft(); return }
  const restoringID = draft.id
  let loaded
  try {
    loaded = await draft.load()
  } catch (value) {
    if (version === loadVersion) {
      draftStatus.value = 'error'
      draftError.value = value instanceof Error ? value.message : 'The saved draft could not be restored.'
    }
    return
  }
  if (!loaded || version !== loadVersion || !session.unlocked || draft !== draftSession || draft.id !== restoringID || session.sentDraftID === restoringID) return
  applyingDraft = true
  try {
    const { meta, attachments: savedAttachments } = loaded
    action.value = meta.action
    original.value = meta.original
    inReplyTo.value = meta.inReplyTo
    to.value = meta.to; cc.value = meta.cc; bcc.value = meta.bcc
    subject.value = meta.subject; text.value = meta.text; html.value = meta.html
    showCc.value = meta.showCc; showBcc.value = meta.showBcc
    if (meta.smtpConfigID && configs.value.some(config => config.id === meta.smtpConfigID)) selectedID.value = meta.smtpConfigID
    signatureID.value=meta.signatureID||'';signatureText.value=meta.signatureText||''
    remoteDraft.value=meta.remoteDraft||null;remoteUnconfirmed.value=meta.remoteUnconfirmed||false;remoteAccountID.value=meta.remoteDraft?.accountID||remoteAccountID.value
    const files = savedAttachments.map(draftAttachmentFile)
    draftAttachmentIDs = new WeakMap()
    attachments.value = files
    savedAttachments.forEach((saved, index) => draftAttachmentIDs.set(files[index]!, saved.id))
    await nextTick()
    if (meta.selectedSentID) {
      if (accounts.value.some(account => account.id === meta.selectedSentID)) selectedSentID.value = meta.selectedSentID
    } else selectedSentID.value = ''
    draftRestored.value = true
    draftStatus.value = 'idle'
    draftError.value = ''
    draftDirty.value = false
    await nextTick()
  } finally { applyingDraft = false }
}

async function discardDraft() {
  clearDraftTimer()
  const draft = draftSession
  if (draft) { await draft.clear(); pendingDraftID.value = draft.id }
  applyingDraft = true
  try {
    clearComposeState()
    draftRestored.value = false
    draftDirty.value = false
    draftStatus.value = 'idle'
    draftError.value = ''
    await nextTick()
  } finally { applyingDraft = false }
}

// A send accepted before a lock can finish while a new composer has already
// restored the same encrypted draft. Remove that stale copy instead of letting
// the user send it a second time.
async function discardSentDraft() {
  const id = session.sentDraftID
  if (!id || !draftSession || draftSession.id !== id) return
  await discardDraft()
  if (session.sentDraftID === id) session.sentDraftID = ''
}

async function loadIdentities() {
  const version = ++loadVersion
  configs.value = [];contacts.value=[]
  selectedID.value = ''
  password.value = ''
  if (!session.unlocked) return
  loading.value = true
  error.value = ''
  sentCopyError.value=''
  try {
    const [savedSenders,savedAccounts]=await Promise.all([listSMTPConfigs(),listAccounts().catch(()=>{
      if(version===loadVersion && session.unlocked)sentCopyError.value='Linked mailbox settings could not be loaded. Sent copies are unavailable for this draft.'
      return {}
    })])
    const saved = Object.values(savedSenders)
    if (version !== loadVersion || !session.unlocked) return
    if (!saved.length) {
      await router.replace('/settings?add=sender#identities')
      return
    }
    configs.value = saved
    accounts.value = Object.values(savedAccounts)
    remoteAccountID.value=(accounts.value.find(item=>item.smtp_config_id===selectedID.value&&item.folder_mapping?.drafts!==null)||accounts.value.find(item=>item.folder_mapping?.drafts!==null))?.id||''
    void listContacts().then(values=>{if(version===loadVersion&&session.unlocked)contacts.value=values}).catch(()=>{})
    selectedID.value = (saved.find(config => config.id === props.smtpConfigID) || saved.find(config => config.is_default) || saved[0])?.id || ''
    const pending = mailActions.pending
    if (pending) {
      draftInitialized = true
      ensureDraftSession()
      applyingDraft = true
      const forwarded: AttachmentFile[] = []
      try {
        mailActions.clear(); action.value = pending.action; original.value = pending.reference
        if (pending.replyFromRecipient && (pending.action === 'reply' || pending.action === 'reply-all')) {
          selectedID.value = recipientReplySender(saved,pending.message)?.id || selectedID.value
        } else if (saved.some(config => config.id === pending.smtpConfigID)) selectedID.value = pending.smtpConfigID!
        signatureID.value=selected.value?.default_signature_id||'';chooseSignature()
        const draft = actionDraft(pending.action, pending.message, [...saved.map(config => config.sender_email || ''), ...(pending.ownEmails || [])])
        to.value = draft.to; cc.value = draft.cc; showCc.value = !!draft.cc; subject.value = draft.subject; text.value = draft.text; html.value = draft.html; inReplyTo.value = draft.in_reply_to
        if(pending.action==='draft'){
          remoteDraft.value={...pending.reference};remoteAccountID.value=pending.reference.accountID;signatureID.value='';signatureText.value='';bcc.value=draft.bcc||'';showBcc.value=!!bcc.value
          const owner=saved.find(item=>pending.message.from.some(address=>item.sender_email?.toLowerCase()===address.email.toLowerCase()));if(owner)selectedID.value=owner.id
        }
        if (pending.action === 'forward'||pending.action==='draft') {
          forwardIncomplete.value = true
          for (const attachment of pending.message.attachments) {
            const blob = await attachmentBlob(pending.reference,attachment,actionController.signal)
            if (version !== loadVersion || !session.unlocked) return
            const file = new globalThis.File([blob],attachment.filename,{ type: attachment.content_type })
            validateAttachments([...attachments.value,file]); attachments.value.push(file); forwarded.push(file)
          }
          forwardIncomplete.value = false
        }
        await nextTick()
      } finally { applyingDraft = false }
      void persistAddedFiles(forwarded)
      queueDraftSave()
    } else {
      await restoreDraft(version)
    }
  } catch (value) {
    if (version === loadVersion && session.unlocked) error.value = value instanceof Error ? value.message : 'Failed to load sender addresses.'
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

watch([() => session.unlocked, () => props.smtpConfigID], () => {
  if (!session.unlocked) { resetDraftSession(); clearComposeState() }
  void loadIdentities()
}, { immediate: true })
watch(() => session.sentDraftID, () => { void discardSentDraft() })
watch(selectedID, () => { password.value = '';if(!applyingDraft){signatureID.value=selected.value?.default_signature_id||'';chooseSignature()} },{flush:'sync'})
watch([
  () => action.value, () => original.value, () => inReplyTo.value, () => selectedID.value,
  () => to.value, () => cc.value, () => bcc.value, () => showCc.value, () => showBcc.value,
  () => subject.value, () => text.value, () => html.value, () => selectedSentID.value,()=>signatureID.value,()=>signatureText.value,()=>remoteDraft.value,()=>remoteUnconfirmed.value,
], () => queueDraftSave(), { deep: true })

function handleBeforeUnload(event: { preventDefault(): void; returnValue: string }) {
  if (!session.unlocked || !composeHasContent()) return
  if (draftStatus.value === 'error' || draftDirty.value || !draftSession?.enabled) {
    event.preventDefault()
    event.returnValue = ''
  }
}
globalThis.addEventListener('beforeunload', handleBeforeUnload)

const removeNavigationGuard = router.beforeEach(async to => {
  if (to.path === '/compose' || router.currentRoute.value.path !== '/compose') return true
  if (session.unlocked) await flushDraft()
  if (!composeHasContent()) return true
  if (draftStatus.value !== 'error' && !draftDirty.value && draftSession?.enabled) return true
  try {
    return globalThis.confirm('This draft could not be saved in this browser. Leave without it?')
  } catch { return false }
})

onUnmounted(() => {
  unmounted = true
  actionController.abort(); loadVersion++
  clearDraftTimer()
  removeNavigationGuard()
  globalThis.removeEventListener('beforeunload', handleBeforeUnload)
  if (draftSession) { disposeComposeDraftSession(draftSession); draftSession = null }
  configs.value = []; password.value = ''
  clearComposeState()
})

const addresses = (value: string) => value.split(',').map(email => email.trim()).filter(Boolean).map(email => ({ email }))

function addFiles(event: { target: unknown }) {
  const input = event.target as AttachmentInput
  try {
    const previousCount = attachments.value.length
    const next = [...attachments.value, ...Array.from(input.files || [])]
    validateAttachments(next)
    attachments.value = next
    error.value = ''
    draftDirty.value = true
    void persistAddedFiles(next.slice(previousCount))
  } catch (value) {
    error.value = value instanceof Error ? value.message : 'Could not attach the files.'
  } finally {
    input.value = ''
  }
}

async function saveProviderDraft(){
  if(remoteSaving.value||sending.value||submissionPending.value||!selected.value||!remoteAccountID.value||remoteUnconfirmed.value||forwardIncomplete.value||!bodyValid.value)return
  remoteSaving.value=true;error.value='';remoteStatus.value=''
  const version=loadVersion;const account=remoteAccountID.value
  try{
    const files=await encodeAttachments(attachments.value)
    if(version!==loadVersion||!session.unlocked)return
    const body=withSignature(text.value,html.value,signatureText.value)
    const result=await saveRemoteDraft(account,{sender:{name:selected.value.sender_name,email:selected.value.sender_email||''},to:addresses(to.value),cc:addresses(cc.value),bcc:addresses(bcc.value),subject:subject.value,...body,attachments:files,...(inReplyTo.value?{in_reply_to:inReplyTo.value}:{})},remoteDraft.value,actionController.signal)
    if(version!==loadVersion||!session.unlocked)return
    remoteDraft.value=result.uid&&result.uid_validity?{accountID:account,uid:result.uid,validity:result.uid_validity,folder:result.folder}:null
    remoteStatus.value=result.previous_removal==='unconfirmed'?'Draft saved. Removing the previous version could not be confirmed; check the provider for a duplicate.':remoteDraft.value?'Draft saved on the IMAP provider.':'Draft saved, but the provider returned no UID. Check Drafts before creating another version.'
    if(!remoteDraft.value)remoteUnconfirmed.value=true
    await saveDraft();useMailboxStore().refresh()
  }catch(value){
    if(version===loadVersion&&session.unlocked){error.value=value instanceof Error?value.message:'Remote saving could not be confirmed.';if(!(value instanceof APIError)||value.status>=500){remoteUnconfirmed.value=true;await saveDraft()}}
  }finally{remoteSaving.value=false}
}
async function deleteProviderDraft(){
  if(!remoteDraft.value||!remoteDeleteConfirmed.value||remoteSaving.value||sending.value)return
  remoteSaving.value=true;error.value='';const version=loadVersion
  try{await deleteRemoteDraft(remoteDraft.value,actionController.signal);if(version!==loadVersion||!session.unlocked)return;remoteDraft.value=null;remoteDeleteConfirmed.value=false;remoteStatus.value='Remote draft deleted. Editor contents were kept.';await saveDraft();useMailboxStore().refresh()}
  catch(value){if(version===loadVersion&&session.unlocked)error.value=value instanceof Error?value.message:'Deletion could not be confirmed. Refresh Drafts before retrying.'}
  finally{remoteSaving.value=false}
}

function fileSize(bytes: number) {
  return bytes >= 1024 * 1024 ? `${(bytes / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.ceil(bytes / 1024))} KB`
}

function deliveryNotice(result?: SendResult): { message: string; kind: 'success' | 'warning' } {
  if (result?.sent_copy?.status === 'failed') return { message: result.sent_copy.message || 'Message sent, but its Sent copy could not be saved.', kind: 'warning' }
  if (sentCopyError.value) return { message: `Message sent. ${sentCopyError.value}`, kind: 'warning' }
  return { message: result?.sent_copy?.status === 'saved' ? 'Message sent. Copy saved in Sent.' : 'Message sent.', kind: 'success' }
}

async function submit() {
  if (sending.value || remoteSaving.value || submissionPending.value || !selected.value || forwardIncomplete.value || !bodyValid.value) return
  sending.value = true
  error.value = ''
  const version = loadVersion
  // Keep the session even if this component is unmounted while SMTP is
  // running, so an accepted message can still delete its encrypted draft.
  const draft = draftSession
  const draftID = draft?.id || ''
  if (draftID) session.pendingDraftIDs.push(draftID)
  const savedRemote=remoteDraft.value?{...remoteDraft.value}:null
  const ownerUser = session.userID
  const ownerCSRF = session.csrf
  const ownsSession = () => session.authenticated && session.userID === ownerUser && session.csrf === ownerCSRF
  try {
    if (text.value.length > 1048576 || html.value.length > 2097152) throw new Error('The message body is too large. Please shorten it before sending.')
    const files = await encodeAttachments(attachments.value)
    if (version !== loadVersion || !session.unlocked) return
    const copy=selectedSentID.value ? {sent_account_id:selectedSentID.value} : {}
    const result=action.value === 'redirect' && original.value
      ? await redirectMessage(original.value,{ smtp_config_id: selectedID.value, to: addresses(to.value), cc: addresses(cc.value), bcc: addresses(bcc.value),...copy },password.value,sendController.signal)
      : await sendMessage({ smtp_config_id: selectedID.value, to: addresses(to.value), cc: addresses(cc.value), bcc: addresses(bcc.value), subject: subject.value, ...withSignature(text.value,html.value,signatureText.value), attachments: files, ...(inReplyTo.value ? { in_reply_to: inReplyTo.value } : {}),...copy }, password.value)

    // The SMTP server accepted the message. Even if the vault locked or the
    // composer was replaced meanwhile, the outcome is final and the encrypted
    // draft must not be offered for a second send.
    let remoteWarning=''
    if(savedRemote&&ownsSession()){try{await deleteRemoteDraft(savedRemote,new globalThis.AbortController().signal)}catch{remoteWarning='The message was sent, but the remote draft could not be removed. Check Drafts before sending anything again.'}}
    clearDraftTimer()
    if (ownsSession()) {
      try { if (draft) await draft.clear() } catch { /* The ID guard below retries on the next mount. */ }
      if (draftID) session.sentDraftID = draftID
    }
    if (!unmounted && version === loadVersion) {
      applyingDraft = true
      try {
        to.value = ''; cc.value = ''; bcc.value = ''; subject.value = ''; text.value = ''; html.value = '';signatureID.value='';signatureText.value=''
        attachments.value = []
        draftAttachmentIDs = new WeakMap()
        await nextTick()
        draftStatus.value = 'idle'; draftError.value = ''; draftDirty.value = false; draftRestored.value = false
      } finally { applyingDraft = false }
    }
    if (ownsSession()) {
      const notice = remoteWarning?{message:remoteWarning,kind:'warning' as const}:deliveryNotice(result)
      session.deferNotice(notice.message, notice.kind)
      if(result?.sent_copy?.status==='saved')useMailboxStore().refresh()
    }
    if (!unmounted && version === loadVersion && session.unlocked) await router.replace('/mail')
  } catch (value) {
    const message = value instanceof Error ? value.message : 'The message could not be sent.'
    if (!ownsSession()) return
    if (!unmounted && version === loadVersion && session.unlocked) error.value = message
    else session.deferNotice(`Delivery was not confirmed. ${message} Your encrypted draft was kept.`, 'warning')
  } finally {
    if (ownsSession()) session.pendingDraftIDs = session.pendingDraftIDs.filter(id => id !== draftID)
    password.value = ''
    sending.value = false
  }
}
</script>

<template>
  <div class="app-shell">
    <AppHeader />
    <main v-if="!session.unlocked" class="center-state"><VaultUnlock /></main>
    <main v-else class="compose-page">
      <div class="compose-heading"><div><p class="eyebrow">Send email</p><h1>{{ title }}</h1></div><RouterLink class="text-button" to="/mail">Back to inbox</RouterLink></div>
      <p v-if="draftRestored" class="compose-draft-restored" role="status"><span>Draft restored from encrypted browser storage.</span><button type="button" class="text-button" @click="discardDraft">Discard draft</button></p>
      <p v-if="loading">Loading sender addresses…</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="draftError" class="error compose-draft-error" role="alert">{{ draftError }} <button type="button" class="text-button" @click="saveDraft">Retry saving the draft</button></p>
      <p v-if="sentCopyError" class="quiet" role="status">{{ sentCopyError }}</p>
      <p v-if="forwardIncomplete && !loading" class="error">Not all original attachments could be included. Go back and retry, or <button type="button" class="text-button" @click="forwardIncomplete = false; error = ''">continue without the remaining attachments</button>.</p>
      <form v-if="!loading && configs.length" class="compose-sheet" aria-label="New message" @submit.prevent="submit">
        <p v-if="submissionPending" role="status">This message is still being sent. Editing resumes if sending fails.</p>
        <fieldset :disabled="sending || submissionPending || remoteSaving">
          <datalist id="compose-contacts"><option v-for="contact in contacts" :key="contact.id" :value="contact.email">{{contact.name}}</option></datalist>
          <div class="compose-fields">
            <div class="compose-row"><label for="compose-from">From</label><select id="compose-from" v-model="selectedID" required><option v-for="config in configs" :key="config.id" :value="config.id">{{ config.name }} / {{ config.sender_email || 'Sender address missing' }}</option></select></div>
            <div v-if="selected && !selected.password" class="compose-row"><label for="compose-password">SMTP password</label><input id="compose-password" v-model="password" type="password" required autocomplete="off"></div>
            <div class="compose-row"><label for="compose-to">To</label><input id="compose-to" v-model="to" list="compose-contacts" type="email" multiple required spellcheck="false" autocapitalize="off" placeholder="Recipients" autofocus><div v-if="!showCc || !showBcc" class="recipient-toggles"><button v-if="!showCc" type="button" class="recipient-toggle" aria-controls="compose-cc" :aria-expanded="showCc" @click="showCc = true">Cc</button><button v-if="!showBcc" type="button" class="recipient-toggle" aria-controls="compose-bcc" :aria-expanded="showBcc" @click="showBcc = true">Bcc</button></div></div>
            <div v-if="showCc" class="compose-row"><label for="compose-cc">Cc</label><input id="compose-cc" v-model="cc" list="compose-contacts" type="email" multiple spellcheck="false" autocapitalize="off" placeholder="Copy recipients"><button type="button" class="icon-button" aria-label="Remove Cc" @click="showCc = false; cc = ''"><AppIcon name="close" :size="16" /></button></div>
            <div v-if="showBcc" class="compose-row"><label for="compose-bcc">Bcc</label><input id="compose-bcc" v-model="bcc" list="compose-contacts" type="email" multiple spellcheck="false" autocapitalize="off" placeholder="Hidden recipients"><button type="button" class="icon-button" aria-label="Remove Bcc" @click="showBcc = false; bcc = ''"><AppIcon name="close" :size="16" /></button></div>
            <div class="compose-row compose-subject"><label for="compose-subject">Subject</label><input id="compose-subject" v-model="subject" maxlength="998" placeholder="Add a subject"></div>
            <div v-if="linkedMailboxes.length" class="compose-row"><label for="compose-sent-copy">Sent copy</label><select id="compose-sent-copy" v-model="selectedSentID"><option value="">Do not save a copy</option><option v-for="account in linkedMailboxes" :key="account.id" :value="account.id">{{ account.name }} / {{ account.folder_mapping?.sent || 'Sent (automatic)' }}</option></select></div>
          </div>
          <p v-if="action === 'redirect'" class="redirect-explanation">Redirect sends the original message unchanged, with Resent headers identifying your selected sender. Some SMTP providers may reject redirected messages.</p>
          <RichTextEditor v-else v-model="html" v-model:text="text" :disabled="sending||submissionPending||remoteSaving" @update:valid="bodyValid=$event" />
          <div v-if="action!=='redirect'&&selected?.signatures?.length" class="compose-signature"><label>Signature<select v-model="signatureID" @change="chooseSignature"><option value="">None</option><option v-for="item in selected.signatures" :key="item.id" :value="item.id">{{item.name}}</option></select></label><pre v-if="signatureText">{{signatureText}}</pre></div>
          <ul v-if="attachments.length" class="compose-attachments" aria-label="Attachments">
            <li v-for="(file, index) in attachments" :key="index"><AppIcon name="paperclip" :size="17" /><span class="attachment-name">{{ file.name }}</span><span class="attachment-size">{{ fileSize(file.size) }}</span><button type="button" class="icon-button" :aria-label="`Remove attachment ${file.name}`" @click="removeAttachment(index)"><AppIcon name="close" :size="15" /></button></li>
          </ul>
          <div v-if="action!=='redirect'" class="remote-draft-tools">
            <label>Remote Drafts mailbox<select v-model="remoteAccountID" @change="remoteDraft=null;remoteStatus='The previous remote copy, if any, was kept.'"><option value="">None</option><option v-for="account in accounts.filter(item=>item.folder_mapping?.drafts!==null)" :key="account.id" :value="account.id">{{account.name}}</option></select></label>
            <button type="button" :disabled="!remoteAccountID||forwardIncomplete||!bodyValid||remoteUnconfirmed" @click="saveProviderDraft">{{remoteSaving?'Saving…':'Save to IMAP Drafts'}}</button>
            <p v-if="!remoteAccountID" class="quiet">Remote saving requires an IMAP account and an existing Drafts folder. The encrypted browser draft remains available on this browser.</p>
            <p v-if="remoteStatus" role="status">{{remoteStatus}}</p>
            <p v-if="remoteUnconfirmed" class="error">The last save could not be confirmed. Inspect the provider Drafts folder before saving again. <button type="button" @click="remoteUnconfirmed=false;remoteDraft=null">I checked Drafts; start a new save</button></p>
            <details v-if="remoteDraft"><summary>Delete remote draft</summary><label><input v-model="remoteDeleteConfirmed" type="checkbox">Permanently delete this remote draft; keep the current editor contents</label><button type="button" :disabled="!remoteDeleteConfirmed" @click="deleteProviderDraft">Delete remote draft</button></details>
          </div>
          <footer class="compose-toolbar">
            <button type="submit" class="primary compose-send" :disabled="!selected?.sender_email || forwardIncomplete || !bodyValid"><AppIcon name="send" :size="17" />{{ sending ? 'Sending…' : 'Send' }}</button>
            <input ref="fileInput" type="file" multiple hidden aria-label="Choose attachments" @change="addFiles">
            <button v-if="action !== 'redirect'" type="button" class="compose-attach" @click="fileInput?.click()"><AppIcon name="paperclip" :size="19" /><span>Attach files</span></button>
            <span v-if="action !== 'redirect'" class="attachment-limit">10 MB total</span>
            <span v-if="draftStatus === 'saving'" class="draft-status" role="status">Saving draft…</span>
            <span v-else-if="draftStatus === 'error'" class="draft-status draft-status-error" role="status">Draft not saved</span>
            <span v-else-if="draftStatus === 'saved' && composeHasContent()" class="draft-status" role="status">Draft saved</span>
          </footer>
        </fieldset>
      </form>
    </main>
  </div>
</template>
