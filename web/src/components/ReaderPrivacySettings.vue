<script setup lang="ts">
import {t} from '../i18n'
import { onMounted } from 'vue'
import { useReaderPreferences } from '../stores/readerPreferences'
import AppIcon from './AppIcon.vue'
const preferences = useReaderPreferences()
onMounted(() => { void preferences.load() })
async function toggle(event: { target: unknown }) {
  try { await preferences.save({ block_external: (event.target as { checked: boolean }).checked }) } catch { /* Store reports the error. */ }
}
</script>
<template>
  <div class="reader-privacy-settings">
    <h3>{{ t("Email privacy") }}</h3>
    <label class="checkbox-option"><input type="checkbox" :checked="preferences.data.block_external" :disabled="preferences.saving || !preferences.loaded" @change="toggle">{{ t("Block external images by default") }}</label>
    <p class="quiet">{{ t("Use Trust sender or Trust domain on a message to remember its image permission. Scripts, forms and external stylesheets are always blocked.") }}</p>
    <p v-if="preferences.error" class="error" role="alert">{{ t(preferences.error) }}</p>
    <h4>{{ t("Trusted senders") }}</h4>
    <p v-if="!preferences.data.addresses.length" class="quiet">{{ t("No individually trusted senders.") }}</p>
    <ul v-else class="privacy-rules" :aria-label="t('Trusted senders')"><li v-for="address in preferences.data.addresses" :key="address"><AppIcon name="shield" :size="15" /><span>{{ address }}</span><button type="button" class="text-button" :aria-label="t('Remove trust for {0}', {'0': address})" :disabled="preferences.saving" @click="preferences.remove(address, false).catch(() => {})">{{ t("Remove trust") }}</button></li></ul>
    <h4>{{ t("Trusted domains") }}</h4><p class="quiet">{{ t("A rule such as *@test.com allows images from every address at test.com. Subdomains require their own rule.") }}</p>
    <p v-if="!preferences.data.domains.length" class="quiet">{{ t("No trusted domains. Use Trust domain on a message to add one.") }}</p>
    <ul v-else class="privacy-rules" :aria-label="t('Trusted domains')"><li v-for="domain in preferences.data.domains" :key="domain"><AppIcon name="shield" :size="15" /><span>*@{{ domain }}</span><button type="button" class="text-button" :aria-label="t('Remove trust for *@{0}', {'0': domain})" :disabled="preferences.saving" @click="preferences.remove(domain, true).catch(() => {})">{{ t("Remove trust") }}</button></li></ul>
  </div>
</template>
