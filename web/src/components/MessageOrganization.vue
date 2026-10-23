<script setup lang="ts">
import {t} from '../i18n'
import { computed, ref, watch } from 'vue'
import type { MessageRef } from '../api/reading'
import { messageKey } from '../api/mailSources'
import { useMailOrganization } from '../stores/organization'
const props=defineProps<{reference:MessageRef}>(),emit=defineEmits<{snoozed:[]}>()
const organization=useMailOrganization(),tags=ref(''),until=ref(''),busy=ref(false),error=ref('')
const current=computed(()=>organization.data.entries[messageKey(props.reference)])
watch([()=>messageKey(props.reference),()=>current.value?.tags.join(',')],()=>{tags.value=current.value?.tags.join(', ')||'';until.value='';error.value=''},{immediate:true})
async function save(snooze=false,clear=false){
  if(busy.value)return
  const reference={...props.reference};busy.value=true;error.value=''
  try{
    const patch=snooze?{snoozedUntil:clear?0:new Date(until.value).getTime()}:{tags:tags.value.split(',').map(tag=>tag.trim()).filter(Boolean)}
    if(snooze&&!clear&&(!Number.isFinite(patch.snoozedUntil)||patch.snoozedUntil!<=Date.now()))throw new Error('Choose a future date and time.')
    await organization.set(reference,patch)
    if(snooze&&!clear)emit('snoozed')
  }catch(value){error.value=value instanceof Error?value.message:'Could not save message organization.'}
  finally{busy.value=false}
}
</script>
<template><details class="message-organization"><summary>{{ t("Tags and snooze") }}</summary><fieldset :disabled="busy||!organization.loaded">
  <label>{{ t("Tags (comma separated)") }}<input v-model="tags" maxlength="780" :placeholder="t('For example: work, invoices')"></label><button type="button" class="secondary" @click="save()">{{ t("Save tags") }}</button>
  <label>{{ t("Hide until") }}<input v-model="until" type="datetime-local"></label><button type="button" class="secondary" :disabled="!until" @click="save(true)">{{ t("Snooze in this application") }}</button>
  <button v-if="current?.snoozedUntil" type="button" class="text-button" @click="save(true,true)">{{ t("Remove snooze") }}</button>
  <p class="quiet">{{ t("Only encrypted references are saved. Messages stay in their provider folders. Snoozed messages return during your session or when you next open the application.") }}</p>
</fieldset><p v-if="error||organization.error" class="error" role="alert">{{t(error||organization.error)}}</p></details></template>
