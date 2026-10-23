<script setup lang="ts">
import { t } from '../i18n'
import {onUnmounted,ref,watch} from 'vue'
import {api} from '../api/client'
import {readPreference,updatePreference,importContacts,type ContactImport} from '../api/preferences'
import {parseContactFile} from '../api/contactFiles'
import {useSessionStore} from '../stores/session'
type Profile={id:string;name:string;kind:'carddav'|'ldap';url:string;host:string;port:number;security:'tls'|'starttls';username:string;password:string;base_dn:string}
type Profiles={id:string;profiles:Profile[]}
const emit=defineEmits<{changed:[]}>()
const session=useSessionStore(),profiles=ref<Profile[]>([]),options=ref({carddav:false,ldap:false}),selected=ref(''),query=ref(''),busy=ref(false),error=ref(''),notice=ref(''),nextPage=ref(''),results=ref<ContactImport[]>([])
const empty=():Profile=>({id:'',name:'',kind:'carddav',url:'',host:'',port:636,security:'tls',username:'',password:'',base_dn:''})
const form=ref<Profile>(empty());let generation=0,controller=new globalThis.AbortController()
function reset(){generation++;controller.abort();controller=new globalThis.AbortController();profiles.value=[];selected.value='';form.value=empty();nextPage.value='';results.value=[];error.value='';notice.value='';query.value='';busy.value=false}
const active=(g:number)=>g===generation&&session.unlocked
watch(()=>[session.unlocked,session.userID,session.vaultEpoch],async()=>{
 reset();if(!session.unlocked)return;const g=generation
 try{const [saved,available]=await Promise.all([readPreference<Profiles>('external-directories',()=>({id:'external-directories',profiles:[]})),api<{carddav:boolean;ldap:boolean}>('/connectors/options')]);if(active(g)){profiles.value=saved.profiles;options.value=available;if(!available.carddav&&available.ldap)form.value.kind='ldap'}}
 catch(value){if(active(g))error.value=value instanceof Error?value.message:'Could not load connectors.'}
},{immediate:true})
watch(()=>JSON.stringify([form.value,query.value]),()=>{nextPage.value='';results.value=[]})
onUnmounted(reset)
function choose(){nextPage.value='';results.value=[];notice.value='';form.value={...(profiles.value.find(p=>p.id===selected.value)||empty())}}
async function save(){
 if(busy.value||!session.unlocked)return;const g=generation;busy.value=true;error.value=''
 try{
  const profile={...form.value,id:form.value.id||globalThis.crypto.randomUUID(),name:form.value.name.trim()}
  if(!profile.name||profile.name.length>128||!profile.username||!profile.password||profile.password.length>4096)throw new Error('Enter a name, username and password.')
  if(profile.kind==='carddav'){const url=new globalThis.URL(profile.url);if(url.protocol!=='https:'||url.username||url.password||url.hash)throw new Error('Use the exact HTTPS address book collection URL without embedded credentials.')}
  else if(!profile.host||!profile.base_dn||!Number.isInteger(profile.port)||profile.port<1||profile.port>65535)throw new Error('Enter the directory host, port and base DN.')
  const saved=await updatePreference<Profiles>('external-directories',()=>({id:'external-directories',profiles:[]}),record=>{record.profiles=[...record.profiles.filter(p=>p.id!==profile.id),profile];if(record.profiles.length>8)throw new Error('Use at most eight external directories.')})
  if(active(g)){profiles.value=saved.profiles;selected.value=profile.id;form.value=profile;notice.value='Connection settings saved encrypted. No contacts were imported.'}
 }catch(value){if(active(g))error.value=value instanceof Error?value.message:'Could not save directory.'}finally{if(active(g))busy.value=false}
}
async function forget(){if(busy.value||!selected.value)return;const g=generation,id=selected.value;busy.value=true
 try{const saved=await updatePreference<Profiles>('external-directories',()=>({id:'external-directories',profiles:[]}),r=>{r.profiles=r.profiles.filter(p=>p.id!==id)});if(active(g)){profiles.value=saved.profiles;selected.value='';choose();notice.value='Connection forgotten. Locally imported contacts are kept.'}}
 catch(value){if(active(g))error.value=value instanceof Error?value.message:'Could not forget connection.'}finally{if(active(g))busy.value=false}}
async function fetchContacts(more=false){
 if(busy.value||!session.unlocked)return;const g=generation,page=more?nextPage.value:'';busy.value=true;error.value='';results.value=[];notice.value=''
 try{
  const result=await api<{contacts:{name:string;email:string}[];cards:{href:string;etag:string;data:string}[];truncated:boolean;next_page?:string}>('/contacts/external',{method:'POST',signal:controller.signal,body:JSON.stringify({...form.value,query:query.value,page_cookie:page,lock_revision:session.serverLockRevision})})
  const entries:ContactImport[]=result.contacts.map(c=>({...c,groups:[]}));let skipped=0
  for(const card of result.cards){const parsed=parseContactFile(card.data);entries.push(...parsed.entries);skipped+=parsed.skipped}
  if(active(g)){nextPage.value=result.next_page||'';results.value=entries.slice(0,100);notice.value=t("{0} addresses available for preview. {1}{2}", {"0": results.value.length, "1": result.truncated||entries.length>100?t('Results limited; narrow the directory query or use an export for the full address book. '):'', "2": skipped?t("{0} unsupported entries skipped.",{"0":skipped}):''})}
 }catch(value){if(active(g))error.value=value instanceof Error?value.message:'Could not read the directory.'}finally{if(active(g))busy.value=false}
}
async function collect(){if(busy.value||!session.unlocked||!results.value.length)return;const g=generation;busy.value=true;error.value=''
 try{const result=await importContacts(results.value);if(active(g)){notice.value=t("{0} contacts added; {1} existing addresses merged.", {"0": result.added, "1": result.duplicates});emit('changed')}}
 catch(value){if(active(g))error.value=value instanceof Error?value.message:'Import failed.'}finally{if(active(g))busy.value=false}}
</script>
<template>
 <details v-if="session.unlocked" class="external-contacts"><summary>{{ t("External address books (read only)") }}</summary>
 <p>{{ t("Read contacts on demand while the vault is open. Previewed contacts stay separate until you choose to import them. No automatic or bidirectional synchronization.") }}</p>
 <p v-if="!options.carddav&&!options.ldap">{{ t("External directories are disabled by the administrator.") }}</p>
 <fieldset v-else :disabled="busy">
 <label>{{ t("Saved connection") }}<select v-model="selected" @change="choose"><option value="">{{ t("New connection") }}</option><option v-for="p in profiles" :key="p.id" :value="p.id">{{p.name}}</option></select></label>
 <label>{{ t("Name") }}<input v-model="form.name" maxlength="128" /></label>
 <label>{{ t("Type") }}<select v-model="form.kind"><option v-if="options.carddav" value="carddav">{{ t("CardDAV") }}</option><option v-if="options.ldap" value="ldap">{{ t("LDAP directory") }}</option></select></label>
 <label v-if="form.kind==='carddav'">{{ t("Address book collection URL") }}<input v-model="form.url" type="url" :placeholder="t('https://contacts.example/addressbooks/user/default/')" /></label>
 <template v-else><label>{{ t("Host") }}<input v-model="form.host" /></label><label>{{ t("Port") }}<input v-model.number="form.port" type="number" min="1" max="65535" /></label><label>{{ t("Security") }}<select v-model="form.security"><option value="tls">{{ t("Verified TLS") }}</option><option value="starttls">{{ t("Required STARTTLS") }}</option></select></label><label>{{ t("Base DN") }}<input v-model="form.base_dn" /></label><label>{{ t("Search (at least two characters)") }}<input v-model="query" maxlength="256" /></label></template>
 <label>{{ t("Username / bind DN") }}<input v-model="form.username" autocomplete="off" maxlength="512" /></label><label>{{ t("Password") }}<input v-model="form.password" type="password" autocomplete="new-password" maxlength="4096" /></label>
 <div class="button-row"><button type="button" @click="save">{{ t("Save encrypted connection") }}</button><button type="button" class="secondary" @click="fetchContacts()">{{ t("Preview contacts") }}</button><button v-if="selected" type="button" class="secondary" @click="forget">{{ t("Forget connection") }}</button></div>
 </fieldset><p v-if="error" class="error" role="alert">{{t(error)}}</p><p v-if="notice" role="status">{{t(notice)}}</p>
 <button v-if="nextPage&&form.kind==='ldap'" type="button" class="secondary" :disabled="busy" @click="fetchContacts(true)">{{ t("Preview next LDAP page") }}</button>
 <ul v-if="results.length"><li v-for="(contact,index) in results" :key="index">{{contact.name}} &lt;{{contact.email}}&gt;</li></ul>
 <button v-if="results.length" :disabled="busy" @click="collect">{{ t("Import preview into local contacts") }}</button>
 </details>
</template>
<style scoped>.external-contacts{margin-block:1rem;padding:1rem;border:1px solid var(--line);border-radius:10px}.external-contacts fieldset{display:grid;gap:.6rem}.external-contacts ul{max-height:18rem;overflow:auto}.button-row{display:flex;flex-wrap:wrap;gap:.5rem}</style>
