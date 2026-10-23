<script setup lang="ts">
import {t} from '../i18n'
import {inject,onUnmounted,ref,watch} from 'vue'
import {routerKey} from 'vue-router'
import {api} from '../api/client'
import {useSessionStore} from '../stores/session'
import {useSieveSeed} from '../stores/sieveSeed'
import type {Message,MessageRef} from '../api/reading'
const props=defineProps<{message:Message;reference:MessageRef}>(),router=inject(routerKey,undefined),store=useSieveSeed()
const session=useSessionStore(),enabled=ref(false)
const stop=watch(()=>[session.unlocked,session.userID,session.vaultEpoch],async(_,__,cleanup)=>{
  enabled.value=false
  const controller=new globalThis.AbortController(),{vmk,userID,vaultEpoch}=session
  cleanup(()=>controller.abort())
  if(!session.unlocked)return
  try{const options=await api<{sieve:boolean}>('/connectors/options',{signal:controller.signal});if(!controller.signal.aborted&&session.unlocked&&session.vmk===vmk&&session.userID===userID&&session.vaultEpoch===vaultEpoch)enabled.value=options.sieve===true}
  catch{/* Optional server rule action stays hidden when unavailable. */}
},{immediate:true})
onUnmounted(stop)
function rule(){if(!enabled.value||!session.unlocked)return;store.seed={accountID:props.reference.accountID,from:props.message.from.map(a=>a.email),to:props.message.to.map(a=>a.email),subject:props.message.subject};void router?.push('/settings#sieve')}
</script>
<template><button v-if="enabled" type="button" class="secondary" @click="rule">{{ t("Create server rule") }}</button></template>
