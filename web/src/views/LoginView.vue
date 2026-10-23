<script setup lang="ts">
import {t} from '../i18n'
import { onUnmounted, ref } from 'vue'
import { useAuthOptions } from '../composables/useAuthOptions'
import { api,APIError } from '../api/client'
import { loginWithOpaque,type MFARequiredError,type AuthSession } from '../crypto/auth'
import { HTTPS_REQUIRED_MESSAGE, requiresHTTPS } from '../crypto/secureContext'
import { useSessionStore } from '../stores/session'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
const login = ref(''); const password = ref(''); const error = ref(''); const busy = ref(false)
const session = useSessionStore(); const router = useRouter()
const httpsRequired = requiresHTTPS()
const { registrationEnabled, optionsLoaded } = useAuthOptions()
const mfaFlow=ref(''),code=ref('')
let pendingPassword='',pendingLogin='',unmounted=false,expiry:ReturnType<typeof globalThis.setTimeout>|undefined
function clearPending(){globalThis.clearTimeout(expiry);pendingPassword='';pendingLogin='';mfaFlow.value='';code.value=''}
onUnmounted(()=>{unmounted=true;clearPending()})
async function verify(){
  if(busy.value||!mfaFlow.value)return
  busy.value=true;error.value=''
  const password=pendingPassword,login=pendingLogin,flow=mfaFlow.value
  try{
    const result=await api<AuthSession>('/auth/mfa/verify',{method:'POST',body:JSON.stringify({flow_id:flow,code:code.value.trim()})})
    if(unmounted||mfaFlow.value!==flow)return
    clearPending();session.accept(result,login);await session.unlock(password);if(!unmounted)await router.push('/mail')
  }catch(value){if(!unmounted)error.value=value instanceof Error?value.message:'Second factor verification failed.'}
  finally{code.value='';busy.value=false}
}
async function submit() {
  if (busy.value || httpsRequired) return
  const enteredLogin = login.value
  const enteredPassword = password.value
  busy.value = true
  error.value = ''
  try {
    const result = await loginWithOpaque(enteredLogin, enteredPassword)
    if(unmounted)return
    session.accept(result, enteredLogin)
    await session.unlock(enteredPassword)
    await router.push('/mail')
  } catch (value) {
    if(unmounted)return
    if(value instanceof APIError&&value.code==='MFA_REQUIRED'){
      const challenge=value as MFARequiredError
      clearPending();mfaFlow.value=challenge.flowID;pendingPassword=enteredPassword;pendingLogin=enteredLogin
      expiry=globalThis.setTimeout(()=>{clearPending();error.value='Second factor verification expired. Sign in again.'},Math.min(120,challenge.expiresIn||120)*1000)
    }else error.value = value instanceof APIError ? value.message : value instanceof Error ? value.message : 'Authentication failed.'
  } finally {
    password.value = ''
    busy.value = false
  }
}
</script>
<template>
  <main class="auth-page login-page">
    <section class="auth-copy">
      <div class="brand large"><span class="brand-symbol"><AppIcon name="mail" :size="25" /></span>{{ t("Mailbox Aggregator") }}</div>
      <h1>{{ t("All your mail,") }}<br><span>{{ t("in one place.") }}</span></h1>
      <p>{{ t("Connect your IMAP inboxes and SMTP senders. Your credentials stay encrypted in your vault.") }}</p>
      <ul class="auth-benefits" :aria-label="t('Privacy benefits')">
        <li><AppIcon name="shield" :size="19" /><div><strong>{{ t("Encrypted settings") }}</strong><span>{{ t("Encrypted in your browser before they are stored.") }}</span></div></li>
        <li><AppIcon name="lock" :size="19" /><div><strong>{{ t("Private sign-in") }}</strong><span>{{ t("Your main password never leaves your browser.") }}</span></div></li>
        <li><AppIcon name="mail" :size="19" /><div><strong>{{ t("No email archive") }}</strong><span>{{ t("Email content is not stored by the app.") }}</span></div></li>
      </ul>
    </section>
    <section class="auth-card">
      <div class="auth-form-icon"><AppIcon name="lock" :size="23" /></div>
      <h2>{{ t("Sign in") }}</h2><p class="auth-subtitle">{{ t("Open your mail and unlock your vault.") }}</p>
      <p v-if="httpsRequired" class="error" role="alert">{{ t(HTTPS_REQUIRED_MESSAGE) }}</p>
      <form v-if="!mfaFlow" @submit.prevent="submit">
        <label>{{ t("Login identifier") }}<input v-model="login" autocomplete="username" required :placeholder="t('Your login identifier')"></label>
        <label>{{ t("Main password") }}<input v-model="password" type="password" autocomplete="current-password" required :placeholder="t('Enter your password')"></label>
        <p v-if="error" class="error" role="alert">{{ t(error) }}</p>
        <button class="primary" :disabled="busy || httpsRequired">{{ busy ? t("Signing in…") : t("Sign in") }}<AppIcon name="arrow" :size="18" /></button>
      </form>
      <form v-else :aria-label="t('Verify second factor')" @submit.prevent="verify">
        <label>{{ t("Authenticator or recovery code") }}<input v-model="code" autocomplete="one-time-code" maxlength="128" required autofocus></label>
        <p v-if="error" class="error" role="alert">{{t(error)}}</p>
        <button class="primary" :disabled="busy||!code.trim()">{{busy?t("Verifying…"):t("Verify and unlock vault")}}</button>
        <button type="button" class="text-button" :disabled="busy" @click="clearPending">{{ t("Cancel and return to sign in") }}</button>
      </form>
      <p class="quiet">{{ t("No recovery backdoor exists. A lost password means the vault cannot be decrypted.") }}</p>
      <p v-if="optionsLoaded && registrationEnabled" class="auth-switch">{{ t("New here?") }}<RouterLink to="/register">{{ t("Create an account →") }}</RouterLink></p>
    </section>
    <footer class="login-footer"><a href="https://github.com/PHD59fr/mailbox-aggregator" target="_blank" rel="noopener noreferrer"><AppIcon name="code" :size="16" />{{ t("GitHub - mailbox-aggregator") }}</a></footer>
  </main>
</template>
