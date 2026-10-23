<script setup lang="ts">
import {t} from '../i18n'
import { onUnmounted, ref, watch } from 'vue'
import { api } from '../api/client'
import { reauthenticateWithOpaque } from '../crypto/auth'
import { useSessionStore } from '../stores/session'
interface Status{available:boolean;enabled:boolean;recovery_codes_remaining?:number}
const session=useSessionStore(),status=ref<Status|null>(null),login=ref(session.login),password=ref(''),code=ref(''),secret=ref(''),uri=ref(''),codes=ref<string[]>([]),error=ref(''),busy=ref(false)
let generation=0
function clear(){generation++;secret.value='';uri.value='';codes.value=[];password.value='';code.value='';status.value=null;error.value=''}
async function load(){const request=generation;try{const result=await api<Status>('/mfa');if(request===generation&&session.unlocked)status.value=result}catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not load second factor settings.'}}
watch(()=>[session.unlocked,session.vaultEpoch],()=>{clear();if(session.unlocked)void load()},{immediate:true})
onUnmounted(clear)
async function change(action:'enroll'|'confirm'|'disable'|'recovery-codes'){
  if(busy.value||!session.unlocked)return
  const request=generation;busy.value=true;error.value='';codes.value=[]
  try{
    if(action!=='confirm')await reauthenticateWithOpaque(login.value,password.value)
    if(request!==generation||!session.unlocked)return
    if(action==='enroll'){
      const result=await api<{secret:string;uri:string}>('/mfa/enroll',{method:'POST',body:JSON.stringify({lock_revision:session.serverLockRevision})})
      if(request===generation&&session.unlocked){secret.value=result.secret;uri.value=result.uri}
    }else{
      const result=await api<{recovery_codes?:string[]}>('/mfa'+(action==='disable'?'':`/${action}`),{method:action==='disable'?'DELETE':'POST',body:JSON.stringify({code:code.value.trim(),lock_revision:session.serverLockRevision})})
      if(request===generation&&session.unlocked){secret.value='';uri.value='';codes.value=result.recovery_codes||[];await load();session.showNotice(action==='disable'?'Second factor disabled. Other web sessions were signed out.':'Second factor settings saved. Other web sessions were signed out.')}
    }
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Second factor update failed.'}
  finally{password.value='';code.value='';busy.value=false}
}
</script>
<template><section id="mfa"><h2>{{ t("Two-factor authentication") }}</h2>
  <p v-if="!status&&!error">{{ t("Loading second factor settings…") }}</p>
  <p v-if="status&&!status.available" class="quiet">{{ t("Second factor support is unavailable on this instance.") }}</p>
  <template v-if="status?.available">
    <p>{{status.enabled?t("Enabled: an authenticator or recovery code is required after your main password."):t("Optional: keep your current sign-in or add an authenticator as a second factor.")}}</p>
    <p class="quiet">{{ t("This protects web sign-in. Dedicated IMAPS/SMTPS passwords keep their existing independent behavior. Recovery codes replace only the second factor; they cannot decrypt a vault whose main password is lost.") }}</p>
    <fieldset :disabled="busy">
      <template v-if="!secret"><label>{{ t("Login identifier") }}<input v-model="login" autocomplete="username"></label><label>{{ t("Current main password") }}<input v-model="password" type="password" autocomplete="current-password"></label></template>
      <template v-if="secret"><p>{{ t("Add this secret manually in an authenticator (TOTP, six digits, 30 seconds), then confirm its code within five minutes.") }}</p><pre>{{secret}}</pre><a :href="uri">{{ t("Open in an authenticator") }}</a><button type="button" class="text-button" @click="secret='';uri='';code=''">{{ t("Cancel setup") }}</button></template>
      <label v-if="secret||status.enabled">{{secret?t("Authenticator code"):t("Authenticator or recovery code")}}<input v-model="code" autocomplete="one-time-code" maxlength="128"></label>
      <button v-if="!status.enabled&&!secret" type="button" class="secondary" :disabled="!password" @click="change('enroll')">{{ t("Set up authenticator") }}</button>
      <button v-if="secret" type="button" class="primary" :disabled="!code.trim()" @click="change('confirm')">{{ t("Confirm and enable") }}</button>
      <template v-if="status.enabled"><p>{{status.recovery_codes_remaining}} {{ t("unused recovery codes remain.") }}</p><button type="button" class="secondary" :disabled="!password||!code.trim()" @click="change('recovery-codes')">{{ t("Replace recovery codes") }}</button><button type="button" class="text-button" :disabled="!password||!code.trim()" @click="change('disable')">{{ t("Disable second factor") }}</button></template>
    </fieldset>
    <div v-if="codes.length" role="status"><p>{{ t("Save these one-use recovery codes now. They will not be displayed again.") }}</p><pre>{{codes.join('\n')}}</pre><button type="button" class="secondary" @click="codes=[]">{{ t("I saved the recovery codes") }}</button></div>
  </template>
  <p v-if="error" class="error" role="alert">{{t(error)}} <button v-if="!status" type="button" @click="load">{{ t("Retry") }}</button></p>
</section></template>
