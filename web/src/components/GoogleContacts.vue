<script setup lang="ts">
import { t } from '../i18n'
import {computed,onUnmounted,ref,watch} from 'vue'
import {api} from '../api/client'
import {oauthGrantList} from '../api/oauth'
import {googleContactPage} from '../api/googleContacts'
import {importContacts,type Contact,type ContactImport} from '../api/preferences'
import {useSessionStore} from '../stores/session'
const props=withDefaults(defineProps<{recipientMode?:boolean;existing?:string[];limit?:number}>(),{recipientMode:false,existing:()=>[],limit:100})
const emit=defineEmits<{changed:[];select:[contacts:Contact[]]}>(),session=useSessionStore(),enabled=ref(false),grants=ref<{id:string;email:string}[]>([]),selected=ref(''),contacts=ref<ContactImport[]>([]),next=ref(''),busy=ref(false),error=ref(''),notice=ref(''),chosen=ref<string[]>([]);let generation=0,controller=new globalThis.AbortController()
const existing=computed(()=>new Set(props.existing.map(email=>email.toLowerCase())))
const available=computed(()=>contacts.value.filter(c=>!existing.value.has(c.email.toLowerCase())))
function clear(){generation++;controller.abort();controller=new globalThis.AbortController();grants.value=[];selected.value='';contacts.value=[];chosen.value=[];next.value='';error.value='';notice.value='';busy.value=false}
const active=(g:number)=>g===generation&&session.unlocked
watch(()=>[session.unlocked,session.vaultEpoch,session.userID],async()=>{clear();if(!session.unlocked)return;const g=generation
 try{const [available,saved]=await Promise.all([api<{google_contacts:boolean}>('/connectors/options'),oauthGrantList()]);if(active(g)){enabled.value=available.google_contacts;grants.value=saved.filter(s=>s.provider==='google'&&s.scope?.split(' ').includes('https://www.googleapis.com/auth/contacts.readonly'))}}
 catch(value){if(active(g))error.value=value instanceof Error?value.message:'Could not load Google connections.'}
},{immediate:true});onUnmounted(clear)
watch(selected,()=>{contacts.value=[];chosen.value=[];next.value='';notice.value=''})
watch(()=>[props.existing,props.limit],()=>{chosen.value=chosen.value.filter(email=>!existing.value.has(email)).slice(0,Math.max(0,props.limit))})
async function preview(more=false){if(busy.value||!session.unlocked||!selected.value)return;const g=generation,id=selected.value;busy.value=true;error.value='';contacts.value=[];chosen.value=[]
 try{const result=await googleContactPage(id,more?next.value:'',controller.signal)
 if(active(g)&&id===selected.value){const seen=new Set<string>();contacts.value=result.contacts.filter(c=>{const key=c.email.toLowerCase();if(seen.has(key))return false;seen.add(key);return true}).map(c=>({...c,groups:[]}));chosen.value=[];next.value=result.next_page;notice.value=props.recipientMode?'Select recipients without importing them into local contacts.':'This page is a read-only preview. Import explicitly to keep contacts in your local address book.'}}
 catch(value){if(active(g))error.value=value instanceof Error?value.message:'Google contacts could not be read.'}finally{if(active(g))busy.value=false}}
async function collect(){if(busy.value||!session.unlocked)return;const g=generation;busy.value=true;error.value=''
 try{const saved=await importContacts(contacts.value);if(active(g)){notice.value=t("{0} contacts added; {1} existing addresses merged.", {"0": saved.added, "1": saved.duplicates});emit('changed')}}catch(value){if(active(g))error.value=value instanceof Error?value.message:'Import failed.'}finally{if(active(g))busy.value=false}}
function pick(){if(!session.unlocked||busy.value||!chosen.value.length||chosen.value.length>props.limit)return
 const values=available.value.filter(c=>chosen.value.includes(c.email.toLowerCase())).slice(0,Math.max(0,props.limit)).map(c=>({id:`google:${selected.value}:${c.email.toLowerCase()}`,name:c.name,email:c.email}))
 if(values.length)emit('select',values)
}
</script>
<template><details v-if="enabled"><summary>{{ t("Google contacts (read only)") }}</summary><p>{{ t("Connect Google in Settings and explicitly request read-only contacts consent. Contacts are fetched only while unlocked.") }}</p><label>{{ t("Authorized account") }}<select v-model="selected" :disabled="busy"><option value="">{{ t("Choose account") }}</option><option v-for="grant in grants" :key="grant.id" :value="grant.id">{{grant.email}}</option></select></label><button type="button" class="secondary" :disabled="busy||!selected" @click="preview()">{{ t("Preview first page") }}</button><button v-if="next" type="button" class="secondary" :disabled="busy" @click="preview(true)">{{ t("Preview next page") }}</button><ul><li v-for="contact in contacts" :key="contact.email"><label v-if="recipientMode"><input v-model="chosen" type="checkbox" :value="contact.email.toLowerCase()" :disabled="busy||existing.has(contact.email.toLowerCase())||!chosen.includes(contact.email.toLowerCase())&&chosen.length>=limit"><span>{{contact.name}} &lt;<bdi dir="ltr">{{contact.email}}</bdi>&gt;</span></label><span v-else>{{contact.name}} &lt;<bdi dir="ltr">{{contact.email}}</bdi>&gt;</span></li></ul><button v-if="recipientMode" type="button" :disabled="busy||!chosen.length||chosen.length>limit" @click="pick">{{ t("Add selected Google recipients") }}</button><button v-else-if="contacts.length" type="button" :disabled="busy" @click="collect">{{ t("Import this page into local contacts") }}</button><p v-if="notice" role="status">{{t(notice)}}</p><p v-if="error" class="error" role="alert">{{t(error)}}</p></details></template>
