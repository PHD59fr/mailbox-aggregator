<script setup lang="ts">
import { ref } from 'vue'
import { useAuthOptions } from '../composables/useAuthOptions'
import { APIError } from '../api/client'
import { registerWithOpaque } from '../crypto/auth'
import { HTTPS_REQUIRED_MESSAGE, requiresHTTPS } from '../crypto/secureContext'
import { useSessionStore } from '../stores/session'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
const login = ref(''); const password = ref(''); const confirmation = ref(''); const accepted = ref(false); const error = ref(''); const busy = ref(false)
const session = useSessionStore(); const router = useRouter()
const httpsRequired = requiresHTTPS()
const { registrationEnabled, optionsLoaded } = useAuthOptions()
async function submit() {
  if (busy.value || httpsRequired || !registrationEnabled.value) return
  if (password.value !== confirmation.value) {
    error.value = 'Passwords do not match.'
    return
  }
  const enteredLogin = login.value
  const enteredPassword = password.value
  busy.value = true
  error.value = ''
  try {
    const result = await registerWithOpaque(enteredLogin, enteredPassword)
    session.accept(result, enteredLogin)
    await session.unlock(enteredPassword)
    await router.push('/mail')
  } catch (value) {
    error.value = value instanceof APIError ? value.message : value instanceof Error ? value.message : 'Registration failed.'
  } finally {
    password.value = ''
    confirmation.value = ''
    busy.value = false
  }
}
</script>
<template>
  <main class="auth-page">
    <section class="auth-copy">
      <div class="brand large"><span class="brand-symbol"><AppIcon name="mail" :size="25" /></span>Mailbox Aggregator</div>
      <h1>Your mail,<br><span>together.</span></h1>
      <p>Receive with IMAP, send with SMTP. Your connection settings are encrypted in your browser before they are stored.</p>
    </section>
    <section class="auth-card">
      <div class="auth-form-icon"><AppIcon name="shield" :size="23" /></div>
      <h2>Create your vault</h2><p class="auth-subtitle">Choose your login and the password that protects your settings.</p>
      <p v-if="httpsRequired" class="error" role="alert">{{ HTTPS_REQUIRED_MESSAGE }}</p>
      <p v-if="optionsLoaded && !registrationEnabled" class="error" role="alert">Registration is closed on this instance.</p>
      <form @submit.prevent="submit">
        <label>Login identifier<input v-model="login" autocomplete="username" required placeholder="Choose your login identifier"></label>
        <label>Main password<input v-model="password" type="password" minlength="12" autocomplete="new-password" required placeholder="At least 12 characters"></label>
        <label>Confirm password<input v-model="confirmation" type="password" autocomplete="new-password" required placeholder="Enter your password again"></label>
        <label class="check"><input v-model="accepted" type="checkbox" required><span>I understand that a lost password cannot be recovered.</span></label>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <button class="primary" :disabled="busy || !accepted || httpsRequired || !registrationEnabled">{{ busy ? 'Creating secure record…' : 'Create account' }}<AppIcon name="arrow" :size="18" /></button>
      </form>
      <p class="auth-switch">Already have a workspace?<RouterLink to="/login">Sign in →</RouterLink></p>
    </section>
  </main>
</template>
