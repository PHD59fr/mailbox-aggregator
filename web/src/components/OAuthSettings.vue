<script setup lang="ts">
import {t} from '../i18n'
import { onUnmounted, ref, watch } from 'vue'
import { useRoute,useRouter } from 'vue-router'
import { api,APIError } from '../api/client'
import { useSessionStore } from '../stores/session'
import { useMailboxStore } from '../stores/mailboxes'
import { storeOAuthGrant,oauthGrantList,removeOAuthGrant,type OAuthGrant,type OAuthReference } from '../api/oauth'
import { readCollection } from '../api/vaultCollection'
import { resetCredentials } from '../api/credentials'
import { providerProfiles } from '../api/providerProfiles'
import type { Account,SMTPConfig } from '../api/accounts'

const props=defineProps<{accounts:Account[];senders:SMTPConfig[]}>(),emit=defineEmits<{changed:[]}>()
const session=useSessionStore(),mailbox=useMailboxStore(),route=useRoute(),router=useRouter()
const options=ref<Record<string,{available:boolean;callback_url:string}>>({}),grants=ref<{id:string;provider:string;email:string}[]>([])
const includeContacts=ref(false)
const provider=ref<'google'|'microsoft'>('google'),email=ref(''),name=ref(''),microsoftType=ref<'outlook'|'microsoft365'>('outlook'),targetAccount=ref(''),targetSender=ref('')
const result=ref<OAuthGrant|null>(null),busy=ref(false),error=ref(''),receipt=ref(''),reference=ref<OAuthReference|null>(null)
let generation=0,newAccountID=globalThis.crypto.randomUUID(),newSenderID=globalThis.crypto.randomUUID()
function clear(){generation++;options.value={};grants.value=[];result.value=null;reference.value=null;email.value='';name.value='';targetAccount.value='';targetSender.value='';receipt.value='';error.value='';newAccountID=globalThis.crypto.randomUUID();newSenderID=globalThis.crypto.randomUUID()}
async function load(){
  const request=generation
  try{
    const [settings,saved]=await Promise.all([api<typeof options.value>('/oauth/options'),oauthGrantList()])
    if(request===generation&&session.unlocked){options.value=settings;grants.value=saved}
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not load OAuth configuration.'}
}
async function take(){
  const state=route.query.oauth_result
  if(typeof state!=='string'||!session.unlocked||busy.value)return
  const request=generation;busy.value=true;error.value=''
  try{
    const tokens=await api<OAuthGrant>('/oauth/result',{method:'POST',body:JSON.stringify({state})})
    if(request!==generation||!session.unlocked)return
    result.value=tokens;email.value=tokens.email;provider.value=tokens.provider;receipt.value='Authorization received. Confirm the mailbox type and save the connection below.'
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Authorization could not be completed.'}
  finally{if(request===generation){const query={...route.query};delete query.oauth_result;await router.replace({path:route.path,query,hash:'#oauth'});busy.value=false}}
}
watch(()=>[session.unlocked,session.vaultEpoch],()=>{clear();if(session.unlocked){void load();void take()}},{immediate:true})
watch(()=>route.query.oauth_result,()=>{void take()})
onUnmounted(clear)
async function connect(){
  if(busy.value||!session.unlocked)return
  const request=generation;busy.value=true;error.value=''
  try{
    const response=await api<{authorization_url:string}>('/oauth/start',{method:'POST',body:JSON.stringify({provider:provider.value,email:email.value.trim(),lock_revision:session.serverLockRevision,contacts:provider.value==='google'&&includeContacts.value})})
    if(request===generation&&session.unlocked)globalThis.location.assign(response.authorization_url)
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not start authorization.'}
  finally{busy.value=false}
}
async function save(){
  if(busy.value||!result.value||!session.unlocked)return
  const request=generation,grant={...result.value},chosenName=name.value.trim();busy.value=true;error.value=''
  try{
    const profile=providerProfiles[grant.provider==='google'?'gmail':microsoftType.value]
    // IMAP authentication verifies the typed mailbox against the actual grant.
    // SMTP AUTH is qualified separately; the probe has a shared IP cooldown.
    if(!reference.value){
      await api('/connections/test',{method:'POST',body:JSON.stringify({protocol:'imap',host:profile.imap.host,port:profile.imap.port,security:profile.imap.security,username:grant.email,oauth_token:grant.access_token})})
      if(request!==generation||!session.unlocked)return
      reference.value=await storeOAuthGrant(grant)
    }
    if(request!==generation||!session.unlocked)return
    const oauth=reference.value!,senderID=targetSender.value||newSenderID,accountID=targetAccount.value||newAccountID
    await resetCredentials()
    if(request!==generation||!session.unlocked)return
    for(let attempt=0;attempt<3;attempt++){
      const collection=await readCollection<SMTPConfig>('smtp'),existing=collection.records[senderID]
      if(!existing&&Object.keys(collection.records).length>=session.maxAccounts)throw new Error('The sender limit is reached. Choose an existing sender or remove one first.')
      collection.records[senderID]={...existing,id:senderID,name:chosenName||existing?.name||grant.email,host:profile.smtp.host,port:profile.smtp.port,security:profile.smtp.security,username:grant.email,sender_email:grant.email,is_default:existing?.is_default??Object.keys(collection.records).length===0,oauth}
      delete collection.records[senderID]!.password
      try{await collection.save();break}catch(value){if(attempt<2&&value instanceof APIError&&value.code==='REVISION_CONFLICT')continue;throw value}
    }
    if(request!==generation||!session.unlocked)return
    receipt.value='The encrypted sending identity was saved. Saving the receiving mailbox…'
    for(let attempt=0;attempt<3;attempt++){
      const collection=await readCollection<Account>('accounts'),existing=collection.records[accountID]
      if(!existing&&Object.keys(collection.records).length>=session.maxAccounts)throw new Error('The sender was saved, but the mailbox limit is reached. Select an existing mailbox before retrying.')
      collection.records[accountID]={...existing,id:accountID,name:chosenName||existing?.name||grant.email,imap_host:profile.imap.host,imap_port:profile.imap.port,imap_security:profile.imap.security,imap_username:grant.email,smtp_config_id:senderID,is_default:existing?.is_default??Object.keys(collection.records).length===0,oauth}
      delete collection.records[accountID]!.password
      delete collection.records[accountID]!.imap_sasl
      try{await collection.save();break}catch(value){if(attempt<2&&value instanceof APIError&&value.code==='REVISION_CONFLICT')continue;throw value}
    }
    if(request!==generation||!session.unlocked)return
    result.value=null;reference.value=null;receipt.value='OAuth mailbox and sender saved. SMTP permissions are checked when you test or send. Folder synchronization keeps its existing opt-in setting.'
    newAccountID=globalThis.crypto.randomUUID();newSenderID=globalThis.crypto.randomUUID();emit('changed');mailbox.refresh();await load()
  }catch(value){if(request===generation)error.value=(value instanceof Error?value.message:'Saving could not be confirmed.')+' Existing settings were preserved except for any identity explicitly saved above. Inspect the listed identities before reconnecting; retrying here reuses the same record IDs.'}
  finally{busy.value=false}
}
async function forget(id:string,revoke:boolean){
  if(busy.value||!session.unlocked)return
  busy.value=true;error.value='';const request=generation
  try{await resetCredentials();await removeOAuthGrant(id,revoke);if(request===generation&&session.unlocked){receipt.value='Local grant removed. Linked identities require reconnection. For Microsoft, revoke application consent in your Microsoft account or tenant portal.';await load();mailbox.refresh()}}
  catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Revocation could not be confirmed.'}
  finally{busy.value=false}
}
</script>
<template><section id="oauth"><h2>{{ t("Connect Google or Microsoft") }}</h2>
  <label v-if="provider==='google'"><input v-model="includeContacts" type="checkbox" />{{ t("Also request read-only Google contacts (administrator must enable it)") }}</label>
  <p>{{ t("OAuth is optional. Existing password accounts keep their current behavior. OAuth accounts are available in the web application; the external mail gateways currently require password accounts.") }}</p>
  <p v-if="!options.google?.available&&!options.microsoft?.available" class="quiet">{{ t("The administrator must configure APP_PUBLIC_URL and the provider application client ID before OAuth can be used.") }}</p>
  <form v-if="!result" @submit.prevent="connect"><fieldset :disabled="busy">
    <label>{{ t("Provider") }}<select v-model="provider"><option value="google" :disabled="!options.google?.available">{{ t("Google") }}</option><option value="microsoft" :disabled="!options.microsoft?.available">{{ t("Microsoft") }}</option></select></label>
    <label>{{ t("Mailbox email") }}<input v-model="email" type="email" required maxlength="254"></label>
    <button type="submit" class="secondary" :disabled="!options[provider]?.available||!email.trim()">{{ t("Authorize with provider") }}</button>
  </fieldset></form>
  <fieldset v-else :disabled="busy">
    <p>{{ t("Requested mailbox:") }} {{result.email}}{{ t(". You must authorize this same account with the provider.") }}</p>
    <label v-if="result.provider==='microsoft'">{{ t("Microsoft mailbox type") }}<select v-model="microsoftType"><option value="outlook">{{ t("Outlook.com") }}</option><option value="microsoft365">{{ t("Microsoft 365") }}</option></select></label>
    <label>{{ t("Connection name") }}<input v-model="name" maxlength="128" :placeholder="result.email"></label>
    <label>{{ t("Receiving identity") }}<select v-model="targetAccount"><option value="">{{ t("Create a new mailbox") }}</option><option v-for="account in props.accounts" :key="account.id" :value="account.id">{{ t("Update") }} {{account.name}}</option></select></label>
    <label>{{ t("Sending identity") }}<select v-model="targetSender"><option value="">{{ t("Create a new sender") }}</option><option v-for="sender in props.senders" :key="sender.id" :value="sender.id">{{ t("Update") }} {{sender.name}}</option></select></label>
    <p class="quiet">{{ t("Updating an existing identity replaces its connection and removes its saved password. Existing folder mappings, signatures and preferences are kept. OAuth grants are encrypted in your vault and refreshed only while it is unlocked.") }}</p>
    <button type="button" class="primary" @click="save">{{ t("Verify IMAP and save connection") }}</button>
  </fieldset>
  <p v-if="receipt" role="status">{{t(receipt)}}</p><p v-if="error" class="error" role="alert">{{t(error)}}</p>
  <details v-if="grants.length"><summary>{{ t("Manage connected grants") }}</summary><ul><li v-for="grant in grants" :key="grant.id">{{grant.provider}} · {{grant.email}} <button type="button" class="text-button" :disabled="busy" @click="forget(grant.id,false)">{{ t("Forget local grant") }}</button><button v-if="grant.provider==='google'" type="button" class="text-button" :disabled="busy" @click="forget(grant.id,true)">{{ t("Revoke Google access and forget grant") }}</button></li></ul></details>
</section></template>
