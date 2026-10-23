<script setup lang="ts">
import {t} from '../i18n'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSessionStore } from '../stores/session'
import AppIcon from './AppIcon.vue'
import { useMailboxStore } from '../stores/mailboxes'
const session = useSessionStore(); const router = useRouter()
const mailbox = useMailboxStore()
const route = useRoute()
const pageNames: Record<string, string> = { '/settings': 'Settings', '/compose': 'New message', '/contacts': 'Contacts' }
const pageName = computed(() => pageNames[route.path] || 'Inbox')
async function logout() { await session.logout(); await router.push('/login') }
async function searchMail() { if (session.unlocked && route.path !== '/mail') await router.push('/mail') }
async function toggleFilters() { mailbox.filtersOpen = !mailbox.filtersOpen; await searchMail() }
</script>
<template>
  <aside class="app-sidebar" :aria-label="t('Main navigation')">
    <RouterLink class="brand-symbol" to="/mail" :aria-label="t('Mailbox Aggregator')" :title="t('Mailbox Aggregator')"><AppIcon name="mail" :size="25" /></RouterLink>
    <nav>
      <RouterLink to="/mail" class="rail-link" :title="t('Inbox')"><AppIcon name="inbox" /><span class="rail-label">{{ t("Inbox") }}</span></RouterLink>
      <RouterLink to="/compose" class="rail-link" :title="t('Compose')" :aria-label="t('New message')"><AppIcon name="send" /><span class="rail-label">{{ t("Compose") }}</span></RouterLink>
      <RouterLink to="/contacts" class="rail-link" :title="t('Contacts')"><AppIcon name="contacts" /><span class="rail-label">{{ t("Contacts") }}</span></RouterLink>
      <RouterLink to="/settings" class="rail-link" :title="t('Settings')"><AppIcon name="settings" /><span class="rail-label">{{ t("Settings") }}</span></RouterLink>
    </nav>
    <div class="sidebar-bottom">
      <span class="rail-status" :title="session.unlocked ? t('Vault unlocked') : t('Vault locked')"><AppIcon :name="session.unlocked ? 'shield' : 'lock'" /><span class="sr-only">{{ session.unlocked ? t("Vault unlocked") : t("Vault locked") }}</span></span>
      <button class="rail-link" :title="t('Log out')" @click="logout"><AppIcon name="logout" /><span class="rail-label">{{ t("Log out") }}</span></button>
    </div>
  </aside>
  <header class="app-header">
    <div class="header-context"><RouterLink class="brand" to="/mail">{{ t("Mailbox") }} <span>{{ t("Aggregator") }}</span></RouterLink><div class="breadcrumb"><span>{{ t("Workspace") }}</span><AppIcon name="chevron" :size="12" /><strong>{{ t(pageName) }}</strong></div></div>
    <form class="mail-search" role="search" @submit.prevent="searchMail"><label class="search"><AppIcon name="search" :size="18" /><span class="sr-only">{{ t("Search mail") }}</span><input v-model="mailbox.search" type="search" maxlength="512" :placeholder="t('Search your mail')" :disabled="!session.unlocked"></label><button type="button" class="search-filter icon-button" :aria-label="t('Filter mail')" aria-controls="mail-filters" :aria-expanded="mailbox.filtersOpen" :class="{active:mailbox.filtering}" :disabled="!session.unlocked" @click="toggleFilters"><AppIcon name="filter" :size="18" /><span v-if="mailbox.filterCount" class="filter-count">{{ mailbox.filterCount }}</span></button></form>
    <div class="header-actions"><span class="vault-status" :class="{ 'is-unlocked': session.unlocked }"><span></span>{{ session.unlocked ? t("Vault unlocked") : t("Vault locked") }}</span></div>
  </header>
</template>
