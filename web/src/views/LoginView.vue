<script setup lang="ts">
import { ref } from 'vue'
import { useAuthOptions } from '../composables/useAuthOptions'
import { APIError } from '../api/client'
import { loginWithOpaque } from '../crypto/auth'
import { HTTPS_REQUIRED_MESSAGE, requiresHTTPS } from '../crypto/secureContext'
import { useSessionStore } from '../stores/session'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
const login = ref(''); const password = ref(''); const error = ref(''); const busy = ref(false)
const session = useSessionStore(); const router = useRouter()
const httpsRequired = requiresHTTPS()
const { registrationEnabled, optionsLoaded } = useAuthOptions()
async function submit() {
  if (busy.value || httpsRequired) return
  const enteredLogin = login.value
  const enteredPassword = password.value
  busy.value = true
  error.value = ''
  try {
    const result = await loginWithOpaque(enteredLogin, enteredPassword)
    session.accept(result, enteredLogin)
    await session.unlock(enteredPassword)
    await router.push('/mail')
  } catch (value) {
    error.value = value instanceof APIError ? value.message : value instanceof Error ? value.message : 'Authentication failed.'
  } finally {
    password.value = ''
    busy.value = false
  }
}
</script>
<template>
  <main class="auth-page login-page">
    <section class="auth-copy">
      <div class="brand large"><span class="brand-symbol"><AppIcon name="mail" :size="25" /></span>Mailbox Aggregator</div>
      <h1>All your mail,<br><span>in one place.</span></h1>
      <p>Connect your IMAP inboxes and SMTP senders. Your credentials stay encrypted in your vault.</p>
      <ul class="auth-benefits" aria-label="Privacy benefits">
        <li><AppIcon name="shield" :size="19" /><div><strong>Encrypted settings</strong><span>Encrypted in your browser before they are stored.</span></div></li>
        <li><AppIcon name="lock" :size="19" /><div><strong>Private sign-in</strong><span>Your main password never leaves your browser.</span></div></li>
        <li><AppIcon name="mail" :size="19" /><div><strong>No email archive</strong><span>Email content is not stored by the app.</span></div></li>
      </ul>
    </section>
    <section class="auth-card">
      <div class="auth-form-icon"><AppIcon name="lock" :size="23" /></div>
      <h2>Sign in</h2><p class="auth-subtitle">Open your mail and unlock your vault.</p>
      <p v-if="httpsRequired" class="error" role="alert">{{ HTTPS_REQUIRED_MESSAGE }}</p>
      <form @submit.prevent="submit">
        <label>Login identifier<input v-model="login" autocomplete="username" required placeholder="Your login identifier"></label>
        <label>Main password<input v-model="password" type="password" autocomplete="current-password" required placeholder="Enter your password"></label>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <button class="primary" :disabled="busy || httpsRequired">{{ busy ? 'Signing in…' : 'Sign in' }}<AppIcon name="arrow" :size="18" /></button>
      </form>
      <p class="quiet">No recovery backdoor exists. A lost password means the vault cannot be decrypted.</p>
      <p v-if="optionsLoaded && registrationEnabled" class="auth-switch">New here?<RouterLink to="/register">Create an account →</RouterLink></p>
    </section>
    <footer class="login-footer"><a href="https://github.com/PHD59fr/mailbox-aggregator" target="_blank" rel="noopener noreferrer"><AppIcon name="code" :size="16" />GitHub - mailbox-aggregator</a></footer>
  </main>
</template>
