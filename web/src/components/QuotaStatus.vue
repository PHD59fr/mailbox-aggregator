<script setup lang="ts">
import { t, formatNumber } from '../i18n'
import { onUnmounted, ref, watch } from 'vue'
import { access } from '../api/reading'
import { api } from '../api/client'
const props=defineProps<{accountID:string}>()
interface Resource {resource:string;usage:number;limit:number;unit:string}
const result=ref<Resource[]|null>(null),error=ref(''),busy=ref(false)
let controller=new globalThis.AbortController()
watch(()=>props.accountID,()=>{controller.abort();controller=new globalThis.AbortController();result.value=null;error.value='';busy.value=false})
onUnmounted(()=>controller.abort())
async function load(){
  if(busy.value||!props.accountID)return
  controller.abort();controller=new globalThis.AbortController();const signal=controller.signal
  busy.value=true;error.value=''
  try{
    const check=await access(props.accountID,signal)
    const value=await api<{supported:boolean;resources:Resource[]}>(`/mailboxes/${encodeURIComponent(props.accountID)}/quota`,{signal});check()
    result.value=value.supported?value.resources:[]
  }catch(value){if(!signal.aborted)error.value=value instanceof Error?value.message:'Quota unavailable.'}
  finally{if(!signal.aborted)busy.value=false}
}
</script>
<template><div class="quota-status"><button type="button" class="text-button" :disabled="busy" @click="load">{{busy?t("Checking quota…"):t("Check provider quota")}}</button><p v-if="result&&!result.length" class="quiet">{{ t("Quota information is unavailable from this provider.") }}</p><p v-for="(item,index) in result" :key="index" class="quiet">{{item.resource}}: {{formatNumber(item.usage)}} / {{formatNumber(item.limit)}} {{item.unit}}</p><p v-if="error" class="error" role="alert">{{t(error)}}</p></div></template>
