<script setup lang="ts">
import { ref, watch } from 'vue'
import { createSMTPConfig, updateSMTPConfig, type SMTPConfig, type Signature } from '../api/accounts'
import AppIcon from './AppIcon.vue'
import ConnectionTest from './ConnectionTest.vue'

const props = defineProps<{ modelValue: boolean; config?: SMTPConfig | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; success: [] }>()
const defaults = () => ({ name: '', sender_name: '', sender_email: '', host: '', port: 465, security: 'tls' as 'tls' | 'starttls', username: '', password: '' })
const form = ref(defaults())
const replyTo=ref('')
const signatures=ref<Signature[]>([])
const defaultSignature=ref('')
const loading = ref(false)
const error = ref('')
function resetForm(config?: SMTPConfig | null) {
  form.value = config ? { name: config.name, sender_name: config.sender_name || '', sender_email: config.sender_email || '',
    host: config.host, port: config.port, security: config.security, username: config.username, password: '' } : defaults()
  form.value.port = config?.port ?? 465
  replyTo.value=config?.reply_to||''
  signatures.value=(config?.signatures||[]).map(item=>({...item}))
  defaultSignature.value=config?.default_signature_id||''
  error.value = ''
}
watch([() => props.modelValue, () => props.config], ([open]) => { resetForm(open ? props.config : undefined) }, { immediate: true })
watch(() => form.value.security, value => { form.value.port = value === 'tls' ? 465 : 587 }, { flush: 'sync' })

function addSignature(){if(signatures.value.length<12)signatures.value.push({id:globalThis.crypto.randomUUID(),name:'Signature',text:''})}
function close() {
  if (loading.value) return
  form.value = defaults();replyTo.value='';signatures.value=[];defaultSignature.value=''
  emit('update:modelValue', false)
}

async function submit() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const input = { ...form.value, name: form.value.name.trim(), sender_email: form.value.sender_email.trim(), host: form.value.host.trim(), username: form.value.username.trim() }
    const settings={...input,...(replyTo.value||props.config?.reply_to?{reply_to:replyTo.value.trim()}:{}),...(signatures.value.length||props.config?.signatures?.length?{signatures:signatures.value.map(item=>({...item})),default_signature_id:defaultSignature.value}:{})}
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
      <header><h2 id="smtp-title">{{ config ? 'Edit sender address' : 'Add sender address' }}</h2><button type="button" class="close-btn" :disabled="loading" aria-label="Close" @click="close"><AppIcon name="close" :size="17" /></button></header>
      <p>Choose your sender address and SMTP connection.</p>
      <form @submit.prevent="submit">
        <fieldset :disabled="loading">
          <label>Connection name<input v-model="form.name" required maxlength="128" placeholder="Personal or work"></label>
          <label>Sender name<input v-model="form.sender_name" maxlength="128" autocomplete="name"></label>
          <label>Sender email<input v-model="form.sender_email" type="email" required maxlength="254" autocomplete="email"></label>
          <label>SMTP host<input v-model="form.host" required maxlength="253" placeholder="smtp.example.com"></label>
          <label>SMTP security<select v-model="form.security"><option value="tls">TLS (port 465)</option><option value="starttls">STARTTLS (port 587)</option></select></label>
          <label>SMTP port<input v-model.number="form.port" type="number" required min="1" max="65535"></label>
          <label>SMTP username<input v-model="form.username" required maxlength="512" autocomplete="off"></label>
          <label>SMTP password<input v-model="form.password" type="password" :required="!config?.password" :placeholder="config?.password ? 'Leave blank to keep current password' : ''" maxlength="4096" autocomplete="new-password"></label>
          <details><summary>Reply address and signatures</summary>
            <label>Reply-To (optional)<input v-model="replyTo" type="email" maxlength="254"></label>
            <div v-for="item in signatures" :key="item.id" class="signature-entry">
              <label>Signature name<input v-model="item.name" required maxlength="128"></label>
              <label>Signature text<textarea v-model="item.text" maxlength="16384" rows="4"></textarea></label>
              <button type="button" class="text-button" @click="signatures=signatures.filter(value=>value.id!==item.id);if(defaultSignature===item.id)defaultSignature=''">Remove signature</button>
            </div>
            <button type="button" class="text-button" :disabled="signatures.length>=12" @click="addSignature()">Add signature</button>
            <label>Default signature<select v-model="defaultSignature"><option value="">None</option><option v-for="item in signatures" :key="item.id" :value="item.id">{{item.name}}</option></select></label>
          </details>
        </fieldset>
        <p class="quiet">The configuration and SMTP password are encrypted in your vault.</p>
        <ConnectionTest protocol="smtp" :host="form.host" :port="form.port" :security="form.security" :username="form.username" :password="form.password || config?.password || ''" />
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="smtp-actions"><button type="button" class="secondary" :disabled="loading" @click="close">Cancel</button><button class="primary" :disabled="loading">{{ loading ? 'Saving…' : config ? 'Save changes' : 'Save sender address' }}</button></div>
      </form>
    </section>
  </div>
</template>

