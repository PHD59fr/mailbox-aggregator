<script setup lang="ts">
import {t} from '../i18n'
import {onUnmounted,ref,watch} from 'vue'
import {readPreference,updatePreference} from '../api/preferences'
import {emptyRecipients,type RecipientHistory} from '../api/recipientHistory'
import {useSessionStore} from '../stores/session'
const emit=defineEmits<{changed:[]}>(),session=useSessionStore(),settings=ref(emptyRecipients()),busy=ref(false),error=ref('');let generation=0
watch(()=>[session.unlocked,session.vaultEpoch],async()=>{const g=++generation;settings.value=emptyRecipients();error.value='';if(!session.unlocked)return;try{const value=await readPreference<RecipientHistory>('recipient-history',emptyRecipients);if(g===generation&&session.unlocked)settings.value=value}catch(value){if(g===generation)error.value=value instanceof Error?value.message:'Could not load recipient preferences.'}},{immediate:true})
onUnmounted(()=>{generation++;settings.value=emptyRecipients()})
async function save(clear=false){if(busy.value||!session.unlocked)return;const g=generation,remember=settings.value.remember,collect=settings.value.collect;busy.value=true;error.value=''
 try{const saved=await updatePreference<RecipientHistory>('recipient-history',emptyRecipients,r=>{r.remember=remember;r.collect=collect;if(clear||!remember)r.recipients=[]});if(g===generation&&session.unlocked){settings.value=saved;emit('changed')}}catch(value){if(g===generation)error.value=value instanceof Error?value.message:'Could not save recipients.'}finally{busy.value=false}}
</script>
<template><details><summary>{{ t("Recipient suggestions and contact collection") }}</summary><fieldset :disabled="busy"><label><input v-model="settings.remember" type="checkbox" />{{ t("Remember To/Cc recipients after confirmed sends (100 addresses, 30 days)") }}</label><label><input v-model="settings.collect" type="checkbox" />{{ t("Also collect To/Cc recipients into local contacts after confirmed sends") }}</label><p>{{ t("Both options are off by default. Bcc and automatic Bcc are excluded. Disabling collection keeps contacts already added.") }}</p><button type="button" class="secondary" @click="save()">{{ t("Save recipient preferences") }}</button><button type="button" class="secondary" @click="save(true)">{{ t("Clear recent recipients") }}</button></fieldset><p v-if="error" class="error" role="alert">{{t(error)}}</p></details></template>
