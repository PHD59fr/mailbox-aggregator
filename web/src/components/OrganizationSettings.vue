<script setup lang="ts">
import {t} from '../i18n'
import { ref,watch } from 'vue'
import { useMailOrganization } from '../stores/organization'
import { useMailboxStore } from '../stores/mailboxes'
import type { MessageRef } from '../api/reading'
import {useSessionStore} from '../stores/session'
const organization=useMailOrganization(),mailbox=useMailboxStore(),busy=ref(false),error=ref('')
const session=useSessionStore()
watch(()=>[session.unlocked,session.vaultEpoch,session.userID],()=>{if(session.unlocked)void organization.load()},{immediate:true})
async function run(action:()=>Promise<void>){if(busy.value)return;busy.value=true;error.value='';try{await action()}catch(value){error.value=value instanceof Error?value.message:'Could not update message references.'}finally{busy.value=false}}
function remember(event:{target:unknown}){const checked=(event.target as {checked:boolean}).checked;void run(()=>organization.change(record=>{record.rememberRecent=checked}))}
function clearRecent(){void run(()=>organization.change(record=>{for(const [key,mark] of Object.entries(record.entries)){delete mark.visitedAt;if(!mark.tags.length&&!mark.snoozedUntil)delete record.entries[key]}}))}
function remove(reference:MessageRef){void run(()=>organization.remove(reference))}
</script>
<template><div class="organization-settings"><fieldset :disabled="busy||!organization.loaded">
  <label><input type="checkbox" :checked="organization.data.rememberRecent" @change="remember"> {{ t("Remember recently opened messages as encrypted references") }}</label>
  <button type="button" class="text-button" @click="clearRecent">{{ t("Clear recently opened history") }}</button>
  <details><summary>{{ t("Manage saved message references") }}</summary><p class="quiet">{{ t("A provider move or folder reset can invalidate a reference. Removing one clears only its tags, snooze and local history; it does not delete the email.") }}</p>
    <ul><li v-for="(mark,key) in organization.data.entries" :key="key">{{mailbox.accounts.find(account=>account.id===mark.reference.accountID)?.name||t('Removed account')}} / {{mark.reference.folder||'INBOX'}} {{ t("/ UID") }} {{mark.reference.uid}} <button type="button" class="text-button" @click="remove(mark.reference)">{{ t("Remove reference") }}</button></li></ul>
  </details>
</fieldset><p v-if="error||organization.error" class="error" role="alert">{{t(error||organization.error)}} <button v-if="!organization.loaded" type="button" @click="organization.load">{{ t("Retry loading references") }}</button></p></div></template>
