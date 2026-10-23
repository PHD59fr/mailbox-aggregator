<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { api } from '../api/client'
import { defineStore } from 'pinia'
import { useSessionStore } from '../stores/session'
const useTestCooldown = defineStore('connection-test-cooldown', { state: () => ({ until: 0 }) })
const props = defineProps<{ protocol: 'imap' | 'smtp'; host: string; port: number; security: string; username: string; password: string }>()
const cooldown = useTestCooldown(); const session = useSessionStore()
const now = ref(Date.now()); const testing = ref(false); const error = ref(''); const success = ref(false)
const remaining = computed(() => Math.max(0, Math.ceil((cooldown.until - now.value) / 1000)))
const timer = globalThis.setInterval(() => { now.value = Date.now() },1000)
let controller = new globalThis.AbortController()
watch(() => [props.host, props.port, props.security, props.username, props.password], () => { controller.abort(); success.value = false; error.value = ''; testing.value = false })
onUnmounted(() => { globalThis.clearInterval(timer); controller.abort() })
async function test() {
  if (testing.value || remaining.value || !session.unlocked) return
  controller.abort(); controller = new globalThis.AbortController()
  testing.value = true; error.value = ''; success.value = false
  cooldown.until = Date.now() + 30000; now.value = Date.now()
  const signal = controller.signal
  try {
    await api('/connections/test', { method: 'POST', signal, body: JSON.stringify({ protocol: props.protocol, host: props.host.trim(), port: props.port, security: props.security, username: props.username, password: props.password }) })
    if (!signal.aborted) success.value = true
  } catch (value) { if (!signal.aborted) error.value = value instanceof Error ? value.message : 'Connection test failed.' }
  finally { if (!signal.aborted) testing.value = false }
}
</script>
<template><div class="connection-test"><button type="button" class="secondary" :disabled="testing || remaining > 0 || !host || !username || !password" @click="test">{{ testing ? 'Testing connection…' : remaining ? `Test again in ${remaining}s` : 'Test connection' }}</button><span v-if="success" class="connection-success" role="status">Connection and authentication succeeded.</span><p v-if="error" class="error" role="alert">{{ error }}</p></div></template>
