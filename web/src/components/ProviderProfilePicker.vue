<script setup lang="ts">
import {t} from '../i18n'
import { computed, ref } from 'vue'
import { providerProfiles, suggestedProvider, type ProviderProfile } from '../api/providerProfiles'
const props=defineProps<{email:string;disabled?:boolean}>()
const emit=defineEmits<{apply:[profile:typeof providerProfiles[ProviderProfile]]}>()
const selected=ref<ProviderProfile|''>('')
const profile=computed(()=>selected.value||suggestedProvider(props.email))
</script>
<template><details class="provider-profile"><summary>{{ t("Provider settings assistant") }}</summary>
  <label>{{ t("Provider") }}<select v-model="selected" :disabled="disabled"><option value="">{{suggestedProvider(email)?t("Suggested: {0}", {"0": providerProfiles[suggestedProvider(email) as ProviderProfile].name}):t("Choose a provider")}}</option><option v-for="(item,id) in providerProfiles" :key="id" :value="id">{{item.name}}</option></select></label>
  <p v-if="profile" class="quiet">{{t(providerProfiles[profile].note)}}</p>
  <button type="button" class="secondary" :disabled="disabled||!profile" @click="profile&&emit('apply',providerProfiles[profile])">{{ t("Apply host, port and TLS settings") }}</button>
  <p class="quiet">{{ t("Settings change only when you apply them. You can edit them before saving. No discovery request or password is sent to a third party by this assistant.") }}</p>
</details></template>
