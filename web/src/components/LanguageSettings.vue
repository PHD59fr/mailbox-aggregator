<script setup lang="ts">
import {t} from '../i18n'
import {direction,locale,type Direction,type Locale} from '../i18n'
import {useLanguagePreferences} from '../i18n/preferences'
const preferences=useLanguagePreferences()
function change(language:Locale,flow:Direction) {
  void preferences.save(language,flow)
}
</script>
<template>
  <div class="language-settings" :aria-busy="preferences.busy">
    <label>{{ t("Interface language") }}<select :value="locale" :disabled="!preferences.loaded||preferences.busy" @change="change(($event.target as HTMLSelectElement).value as Locale,direction)"><option value="en" lang="en">English</option><option value="fr" lang="fr">Français</option></select></label>
    <label>{{ t("Interface direction") }}<select :value="direction" :disabled="!preferences.loaded||preferences.busy" @change="change(locale,($event.target as HTMLSelectElement).value as Direction)"><option value="auto">{{ t("Automatic") }}</option><option value="ltr">{{ t("Left to right") }}</option><option value="rtl">{{ t("Right to left") }}</option></select></label>
    <p class="quiet">{{ t("Your language and direction are saved in your encrypted profile. Email content is kept as written.") }}</p>
    <p v-if="preferences.error" class="error" role="alert">{{t(preferences.error)}} <button type="button" class="text-button" :disabled="preferences.busy" @click="preferences.load()">{{t("Retry")}}</button></p>
  </div>
</template>
<style scoped>
.language-settings { display:flex; flex-wrap:wrap; gap:12px; align-items:end; }
.language-settings label { flex:1 1 160px; }
.language-settings p { flex-basis:100%; }
</style>
