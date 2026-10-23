<script setup lang="ts">
import {t} from './i18n'
import { onUnmounted, watch } from 'vue'
import { useReaderPreferences } from './stores/readerPreferences'
import {useSieveSeed} from './stores/sieveSeed'
import {useMailOrganization} from './stores/organization'
import { useMailActions } from './stores/mailActions'
import { useRoute } from 'vue-router'
import VaultUnlock from './components/VaultUnlock.vue'
import AppIcon from './components/AppIcon.vue'
import { useSessionStore } from './stores/session'
import { useMailboxMonitoring } from './composables/useMailboxMonitoring'
import {useVaultActivity} from './composables/useVaultActivity'
import { useThemeStore } from './stores/theme'
import { navigationFailure, reloadFailedPage } from './router/recovery'
import {useLanguagePreferences} from './i18n/preferences'
const languages=useLanguagePreferences()
const session = useSessionStore()
const route = useRoute()
const privacy = useReaderPreferences()
const mailActions = useMailActions()
const organization=useMailOrganization()
const appearance=useThemeStore()
watch([()=>session.authenticated,()=>session.unlocked,()=>session.userID,()=>session.vaultEpoch],()=>{
  languages.clear()
  if(session.authenticated && session.unlocked)void languages.load()
  appearance.clear()
  if(session.authenticated && session.unlocked)void appearance.load()
},{immediate:true})
watch([() => session.unlocked, () => session.userID, () => session.vaultEpoch], () => { privacy.clear(); mailActions.clear(); organization.clear();useSieveSeed().clear() })
useVaultActivity()
useMailboxMonitoring(() => !!route.meta.requiresAuth && !session.loading)
onUnmounted(() => { session.dismissNotice();appearance.clear() })
</script>
<template>
  <main v-if="session.loading" class="center-state">{{ t("Loading Mailbox Aggregator…") }}</main>
  <main v-else-if="route.meta.requiresAuth && session.authenticated && !session.unlocked" class="vault-lock-screen">
    <VaultUnlock />
  </main>
  <RouterView v-else />
  <div v-if="navigationFailure && !session.loading" class="navigation-error" role="alert">
    <span>{{ t("This page could not be loaded. Check your connection, then reload to get the latest application.") }}</span>
    <button type="button" @click="reloadFailedPage">{{ t("Reload page") }}</button>
  </div>
  <Teleport to="body">
    <div v-if="session.notice && session.authenticated && session.unlocked && !session.loading && route.meta.requiresAuth" class="mail-notice" :class="{'notice-warning':session.noticeKind==='warning'}" role="status" aria-atomic="true"><AppIcon :name="session.noticeKind==='warning' ? 'shield' : 'check'" :size="18" /><span>{{ t(session.notice) }}</span><button type="button" class="icon-button" :aria-label="t('Dismiss notification')" @click="session.dismissNotice()"><AppIcon name="close" :size="16" /></button></div>
  </Teleport>
</template>
<style scoped>
.navigation-error { position:fixed;z-index:1000;bottom:90px;left:50%;transform:translateX(-50%);width:min(560px,calc(100% - 32px));padding:16px;display:flex;flex-wrap:wrap;align-items:center;gap:12px;background:var(--surface);color:var(--text);border:1px solid var(--line);border-radius:12px;box-shadow:0 8px 32px #0003; }
.navigation-error span { flex:1 1 240px; }
</style>
