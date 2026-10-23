<script setup lang="ts">
import {t} from '../i18n'
import { onUnmounted, ref, watch } from 'vue'
import { updatePreference } from '../api/preferences'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import { type WorkspaceDisplay } from '../api/workspaceDisplay'
import {useWorkspaceDisplay} from '../composables/useWorkspaceDisplay'
const mailbox=useMailboxStore(),session=useSessionStore(),busy=ref(false),error=ref('')
const {error:loadError,loaded}=useWorkspaceDisplay()
let generation=0
watch(()=>[session.unlocked,session.vaultEpoch,session.userID],()=>{generation++;error.value='';busy.value=false})
onUnmounted(()=>{generation++})
async function save(){
  if(busy.value||!session.unlocked||!loaded.value)return
  const request=generation,record=JSON.parse(JSON.stringify(mailbox.display)) as WorkspaceDisplay
  const keys=Object.values(record.shortcuts).filter(Boolean)
  if(keys.some(key=>!(/^[a-z/]$/.test(key)||['Enter','Escape','ArrowDown','ArrowUp'].includes(key)))||new Set(keys).size!==keys.length){error.value='Use distinct lowercase letters, /, Enter, Escape, ArrowDown or ArrowUp. Leave a shortcut empty to disable it.';return}
  busy.value=true;error.value=''
  try{await updatePreference<WorkspaceDisplay>('workspace-display',()=>record,current=>Object.assign(current,record));if(request===generation&&session.unlocked)session.showNotice('Display and keyboard settings saved.')}
  catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not save display settings.'}
  finally{busy.value=false}
}
</script>
<template><details class="workspace-appearance"><summary>{{ t("Display and keyboard") }}</summary><fieldset :disabled="busy||!loaded">
  <label>{{ t("Message density") }}<select v-model="mailbox.display.density"><option value="comfortable">{{ t("Comfortable") }}</option><option value="compact">{{ t("Compact") }}</option><option value="spacious">{{ t("Spacious") }}</option></select></label>
  <label>{{ t("Combined views: period") }}<select v-model.number="mailbox.display.windowDays"><option :value="0">{{ t("All dates") }}</option><option :value="7">{{ t("Last 7 days") }}</option><option :value="30">{{ t("Last 30 days") }}</option><option :value="90">{{ t("Last 90 days") }}</option><option :value="365">{{ t("Last year") }}</option></select></label>
  <label>{{ t("Combined views: loaded messages per folder") }}<select v-model.number="mailbox.display.windowLimit"><option :value="0">{{ t("No extra limit") }}</option><option :value="50">50</option><option :value="100">100</option><option :value="250">250</option><option :value="500">500</option></select></label>
  <label>{{ t("Reading pane") }}<select v-model="mailbox.display.layout"><option value="split">{{ t("Beside the list") }}</option><option value="below">{{ t("Below the list") }}</option></select></label>
  <label v-for="account in mailbox.accounts" :key="account.id">{{account.name}} {{ t("color") }}<select v-model="mailbox.display.colors[account.id]"><option value="">{{ t("Theme default") }}</option><option v-for="color in ['blue','purple','rose','amber','green']" :key="color" :value="color">{{t(color)}}</option></select></label>
  <label><input v-model="mailbox.display.keyboard" type="checkbox"> {{ t("Enable keyboard shortcuts") }}</label>
  <div v-if="mailbox.display.keyboard"><label v-for="(_,action) in mailbox.display.shortcuts" :key="action">{{t(action)}}<input v-model="mailbox.display.shortcuts[action]" maxlength="10" autocomplete="off"></label></div>
  <button type="button" class="secondary" @click="save">{{ t("Save display and keyboard settings") }}</button>
  <p class="quiet">{{ t("Shortcuts are inactive while typing or using dialogs. Settings are encrypted in your vault.") }}</p>
</fieldset><p v-if="error||loadError" class="error" role="alert">{{t(error||loadError)}}</p></details></template>
