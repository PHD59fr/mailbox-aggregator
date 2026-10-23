<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {t} from '../i18n'
import {useMailboxStore} from '../stores/mailboxes'
import {useSessionStore} from '../stores/session'
import {useFolderSync} from '../composables/useFolderSync'
import {sourceKey,type MailSource} from '../api/mailSources'
import {listSavedSearches,saveSearch,deleteSearch,readPreference,updatePreference,type SavedSearch} from '../api/preferences'
import {emptySearchHistory,readSearchHistory,type SearchHistory} from '../api/searchHistory'

interface CombinedFolders {id:'combined-folders';sources:MailSource[]}
const mailbox=useMailboxStore(),session=useSessionStore(),{folders,folderErrors}=useFolderSync()
const searches=ref<SavedSearch[]>([]),history=ref<SearchHistory>(emptySearchHistory()),busy=ref(false),loaded=ref(false),error=ref('')
let generation=0
const choices=computed(()=>!session.unlocked?[]:mailbox.accounts.filter(account=>account.sync_folders).flatMap(account=>[
  {accountID:account.id,folder:'INBOX',label:`${account.name} / ${t('Inbox')}`},
  ...(folders.value[account.id]?.folders||[]).filter(folder=>folder.selectable&&folder.name.toUpperCase()!=='INBOX').map(folder=>({accountID:account.id,folder:folder.name,label:`${account.name} / ${folder.name}`})),
]))
watch(()=>[session.unlocked,session.vaultEpoch,session.userID],async()=>{
  const request=++generation
  searches.value=[];history.value=emptySearchHistory();loaded.value=false;busy.value=false;error.value=''
  if(!session.unlocked)return
  try {
    const [saved,combined,past]=await Promise.all([listSavedSearches(),readPreference<CombinedFolders>('combined-folders',()=>({id:'combined-folders',sources:[]})),readSearchHistory()])
    if(request!==generation||!session.unlocked)return
    searches.value=saved;history.value=past;mailbox.selectedFolders=Array.isArray(combined.sources)?combined.sources.filter(source=>source&&typeof source.accountID==='string'&&typeof source.folder==='string'&&source.folder.length<=1024).slice(0,100):[];loaded.value=true
  } catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not load saved views.'}
},{immediate:true})
onUnmounted(()=>{generation++})
async function run(action:()=>Promise<void>){
  if(busy.value||!loaded.value||!session.unlocked)return
  const request=generation;busy.value=true;error.value=''
  try {await action()}catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not save this view.'}
  finally {if(request===generation)busy.value=false}
}
async function toggle(source:MailSource,checked:boolean){
  const request=generation
  await run(async()=>{
    const result=await updatePreference<CombinedFolders>('combined-folders',()=>({id:'combined-folders',sources:[]}),record=>{
      const remaining=(record.sources||[]).filter(item=>sourceKey(item)!==sourceKey(source))
      if(checked&&remaining.length>=100)throw new Error('Select at most 100 folders.')
      record.sources=checked?[...remaining,{accountID:source.accountID,folder:source.folder}]:remaining
    })
    if(request===generation&&session.unlocked)mailbox.selectedFolders=result.sources
  })
}
async function setAlert(search:SavedSearch,enabled:boolean){
  const request=generation
  await run(async()=>{const saved=await saveSearch({...search,alert:enabled});if(request===generation&&session.unlocked)searches.value=saved})
}
async function remove(id:string){
  const request=generation
  await run(async()=>{const saved=await deleteSearch(id);if(request===generation&&session.unlocked)searches.value=saved})
}
async function historySetting(remember:boolean,clear=false){
  const request=generation
  await run(async()=>{
    const saved=await updatePreference<SearchHistory>('search-history',emptySearchHistory,record=>{record.remember=remember;if(clear||!remember)record.searches=[]})
    if(request===generation&&session.unlocked)history.value=saved
  })
}
</script>
<template>
  <div class="search-preferences">
    <fieldset :disabled="busy||!loaded">
      <details><summary>{{t("Folders in the Selected folders view")}}</summary>
        <p class="quiet">{{t("Enable folder synchronization in Settings to select folders. Choices are encrypted in your vault. Renamed folders must be selected again.")}}</p>
        <label v-for="choice in choices" :key="sourceKey(choice)"><input type="checkbox" :checked="mailbox.selectedFolders.some(source=>sourceKey(source)===sourceKey(choice))" @change="toggle(choice,($event.target as HTMLInputElement).checked)"> {{choice.label}}</label>
        <p v-for="(message,id) in folderErrors" :key="id" class="error" role="alert">{{t(message)}}</p>
      </details>
      <details><summary>{{t("Saved search")}}</summary>
        <ul><li v-for="search in searches" :key="search.id"><span>{{search.name}}</span><label><input type="checkbox" :checked="search.alert" @change="setAlert(search,($event.target as HTMLInputElement).checked)"> {{t("Check this saved search for new results while this mail view is open")}}</label><button type="button" class="text-button" @click="remove(search.id)">{{t("Delete saved search")}}</button></li></ul>
        <p class="quiet">{{t("At most three searches, ten folders each, every two minutes while visible and unlocked. The first check sets a baseline. Only loaded result windows are compared; no notification is promised while closed.")}}</p>
      </details>
      <details><summary>{{t("Search history (optional)")}}</summary>
        <label><input type="checkbox" :checked="history.remember" @change="historySetting(($event.target as HTMLInputElement).checked)"> {{t("Remember searches encrypted (20 entries, 30 days)")}}</label>
        <button type="button" class="text-button" @click="historySetting(history.remember,true)">{{t("Clear history")}}</button>
      </details>
    </fieldset>
    <p v-if="error" class="error" role="alert">{{t(error)}}</p>
  </div>
</template>
