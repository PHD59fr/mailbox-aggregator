<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSessionStore } from '../stores/session'
import AppIcon from './AppIcon.vue'
const session = useSessionStore(); const password = ref(''); const busy = ref(false); const error = ref('')
const passwordInput = ref<{ focus(): void } | null>(null)
onMounted(() => { passwordInput.value?.focus() })
async function unlock() { busy.value = true; error.value = ''; try { await session.unlock(password.value); password.value = '' } catch { error.value = 'The vault could not be unlocked. Check the password and vault data.' } finally { busy.value = false } }
</script>
<template>
  <section class="lock-card" aria-labelledby="unlock-title"><div class="lock-mark"><AppIcon name="lock" :size="28" /></div><p class="eyebrow">Your private workspace</p><h1 id="unlock-title">Unlock your mailbox</h1><p>Enter your main password to open your workspace.</p><form @submit.prevent="unlock"><label>Main password<input ref="passwordInput" v-model="password" type="password" autocomplete="current-password" required autofocus placeholder="Enter your password"></label><p v-if="error" class="error" role="alert">{{ error }}</p><button class="primary" :disabled="busy">{{ busy ? 'Unlocking…' : 'Unlock vault' }}<AppIcon name="arrow" :size="17" /></button></form><p class="quiet">Your password stays in this browser.</p></section>
</template>
