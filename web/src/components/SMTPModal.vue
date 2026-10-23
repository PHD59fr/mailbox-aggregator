<script setup lang="ts">
import {t} from '../i18n'
import { ref, watch } from 'vue'
import { createSMTPConfig, updateSMTPConfig, type SMTPConfig, type Signature } from '../api/accounts'
import ProviderProfilePicker from './ProviderProfilePicker.vue'
import AppIcon from './AppIcon.vue'
import ConnectionTest from './ConnectionTest.vue'

const props = defineProps<{ modelValue: boolean; config?: SMTPConfig | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; success: [] }>()
const defaults = () => ({ name: '', sender_name: '', sender_email: '', host: '', port: 465, security: 'tls' as 'tls' | 'starttls', username: '', password: '' })
const form = ref(defaults())
const replyTo=ref('');const autoBcc=ref('')
const signatures=ref<Signature[]>([])
const defaultSignature=ref('')
const loading = ref(false)
const error = ref('')
function resetForm(config?: SMTPConfig | null) {
  form.value = config ? { name: config.name, sender_name: config.sender_name || '', sender_email: config.sender_email || '',
    host: config.host, port: config.port, security: config.security, username: config.username, password: '' } : defaults()
  form.value.port = config?.port ?? 465
  replyTo.value=config?.reply_to||'';autoBcc.value=config?.auto_bcc||''
  signatures.value=(config?.signatures||[]).map(item=>({...item}))
  defaultSignature.value=config?.default_signature_id||''
  error.value = ''
}
watch([() => props.modelValue, () => props.config], ([open]) => { resetForm(open ? props.config : undefined) }, { immediate: true })
watch(() => form.value.security, value => { form.value.port = value === 'tls' ? 465 : 587 }, { flush: 'sync' })

function addSignature(){if(signatures.value.length<12)signatures.value.push({id:globalThis.crypto.randomUUID(),name:'Signature',text:''})}
function close() {
  if (loading.value) return
  form.value = defaults();replyTo.value='';autoBcc.value='';signatures.value=[];defaultSignature.value=''
  emit('update:modelValue', false)
}

async function submit() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const input = { ...form.value, name: form.value.name.trim(), sender_email: form.value.sender_email.trim(), host: form.value.host.trim(), username: form.value.username.trim() }
    const settings={...input,...(autoBcc.value||props.config?.auto_bcc?{auto_bcc:autoBcc.value.trim()}:{}),...(replyTo.value||props.config?.reply_to?{reply_to:replyTo.value.trim()}:{}),...(signatures.value.length||props.config?.signatures?.length?{signatures:signatures.value.map(item=>({...item})),default_signature_id:defaultSignature.value}:{})}
    if (props.config) await updateSMTPConfig(props.config.id, settings)
    else await createSMTPConfig(settings)
    form.value = defaults()
    emit('success')
    emit('update:modelValue', false)
  } catch (value) {
    error.value = value instanceof Error ? value.message : 'Failed to save sender address.'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div v-if="modelValue" class="smtp-backdrop" @click.self="close">
    <section class="smtp-modal" role="dialog" aria-modal="true" aria-labelledby="smtp-title">
      <header><h2 id="smtp-title">{{ config ? t("Edit sender address") : t("Add sender address") }}</h2><button type="button" class="close-btn" :disabled="loading" :aria-label="t('Close')" @click="close"><AppIcon name="close" :size="17" /></button></header>
      <p>{{ t("Choose your sender address and SMTP connection.") }}</p>
      <form @submit.prevent="submit">
        <fieldset :disabled="loading">
          <label>{{ t("Connection name") }}<input v-model="form.name" required maxlength="128" :placeholder="t('Personal or work')"></label>
          <label>{{ t("Sender name") }}<input v-model="form.sender_name" maxlength="128" autocomplete="name"></label>
          <label>{{ t("Sender email") }}<input v-model="form.sender_email" type="email" required maxlength="254" autocomplete="email"></label>
          <label>{{ t("SMTP host") }}<input v-model="form.host" required maxlength="253" :placeholder="t('smtp.example.com')"></label>
          <label>{{ t("SMTP security") }}<select v-model="form.security"><option value="tls">{{ t("TLS (port 465)") }}</option><option value="starttls">{{ t("STARTTLS (port 587)") }}</option></select></label>
          <label>{{ t("SMTP port") }}<input v-model.number="form.port" type="number" required min="1" max="65535"></label>
          <label>{{ t("SMTP username") }}<input v-model="form.username" required maxlength="512" autocomplete="off"></label>
          <label>{{ t("SMTP password") }}<input v-model="form.password" type="password" :required="!config?.password&&!config?.oauth" :placeholder="config?.oauth ? t('Leave blank to keep OAuth; enter a password to switch authentication') : config?.password ? t('Leave blank to keep current password') : ''" maxlength="4096" autocomplete="new-password"></label>
          <ProviderProfilePicker :email="form.sender_email" :disabled="loading" @apply="profile=>{form.security=profile.smtp.security;form.host=profile.smtp.host;form.port=profile.smtp.port;if(!form.username)form.username=form.sender_email}" />
          <details><summary>{{ t("Reply address and signatures") }}</summary>
            <label>{{ t("Reply-To (optional)") }}<input v-model="replyTo" type="email" maxlength="254"></label>
            <label>{{ t("Automatic Bcc (optional)") }}<input v-model="autoBcc" type="email" maxlength="254"></label>
            <div v-for="item in signatures" :key="item.id" class="signature-entry">
              <label>{{ t("Signature name") }}<input v-model="item.name" required maxlength="128"></label>
              <label>{{ t("Signature text") }}<textarea v-model="item.text" maxlength="16384" rows="4"></textarea></label>
              <button type="button" class="text-button" @click="signatures=signatures.filter(value=>value.id!==item.id);if(defaultSignature===item.id)defaultSignature=''">{{ t("Remove signature") }}</button>
            </div>
            <button type="button" class="text-button" :disabled="signatures.length>=12" @click="addSignature()">{{ t("Add signature") }}</button>
            <label>{{ t("Default signature") }}<select v-model="defaultSignature"><option value="">{{ t("None") }}</option><option v-for="item in signatures" :key="item.id" :value="item.id">{{item.name}}</option></select></label>
          </details>
        </fieldset>
        <p class="quiet">{{ t("The configuration and SMTP password are encrypted in your vault.") }}</p>
        <ConnectionTest :oauth="config?.oauth" protocol="smtp" :host="form.host" :port="form.port" :security="form.security" :username="form.username" :password="form.password || config?.password || ''" />
        <p v-if="error" class="error" role="alert">{{ t(error) }}</p>
        <div class="smtp-actions"><button type="button" class="secondary" :disabled="loading" @click="close">{{ t("Cancel") }}</button><button class="primary" :disabled="loading">{{ loading ? t("Saving…") : config ? t("Save changes") : t("Save sender address") }}</button></div>
      </form>
    </section>
  </div>
</template>

