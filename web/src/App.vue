<script setup lang="ts">
import { onUnmounted, watch } from 'vue'
import { useReaderPreferences } from './stores/readerPreferences'
import { useMailActions } from './stores/mailActions'
import { useRoute } from 'vue-router'
import VaultUnlock from './components/VaultUnlock.vue'
import AppIcon from './components/AppIcon.vue'
import { useSessionStore } from './stores/session'
import { useMailboxMonitoring } from './composables/useMailboxMonitoring'
import {useVaultActivity} from './composables/useVaultActivity'
import { useThemeStore } from './stores/theme'
const session = useSessionStore()
const route = useRoute()
const privacy = useReaderPreferences()
const mailActions = useMailActions()
const appearance=useThemeStore()
watch([()=>session.authenticated,()=>session.unlocked,()=>session.userID,()=>session.vaultEpoch],()=>{
  appearance.clear()
  if(session.authenticated && session.unlocked)void appearance.load()
},{immediate:true})
watch([() => session.unlocked, () => session.userID, () => session.vaultEpoch], () => { privacy.clear(); mailActions.clear() })
useVaultActivity()
useMailboxMonitoring(() => !!route.meta.requiresAuth && !session.loading)
onUnmounted(() => { session.dismissNotice();appearance.clear() })
</script>
<template>
  <main v-if="session.loading" class="center-state">Loading Mailbox Aggregator…</main>
  <main v-else-if="route.meta.requiresAuth && session.authenticated && !session.unlocked" class="vault-lock-screen">
    <VaultUnlock />
  </main>
  <RouterView v-else />
  <Teleport to="body">
    <div v-if="session.notice && session.authenticated && session.unlocked && !session.loading && route.meta.requiresAuth" class="mail-notice" :class="{'notice-warning':session.noticeKind==='warning'}" role="status" aria-atomic="true"><AppIcon :name="session.noticeKind==='warning' ? 'shield' : 'check'" :size="18" /><span>{{ session.notice }}</span><button type="button" class="icon-button" aria-label="Dismiss notification" @click="session.dismissNotice()"><AppIcon name="close" :size="16" /></button></div>
  </Teleport>
</template>
