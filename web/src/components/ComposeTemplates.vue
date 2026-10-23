<script setup lang="ts">
import {t} from '../i18n'
import { onUnmounted, ref, watch } from 'vue'
import { readPreference, updatePreference } from '../api/preferences'
import { useSessionStore } from '../stores/session'
export interface ComposeTemplate { id:string; name:string; to:string; cc:string; subject:string; text:string; html:string }
interface Templates {id:'compose-templates'; templates:ComposeTemplate[]}
const props=defineProps<{draft:Omit<ComposeTemplate,'id'|'name'>;disabled?:boolean}>()
const emit=defineEmits<{insert:[template:ComposeTemplate]}>()
const session=useSessionStore(),templates=ref<ComposeTemplate[]>([]),name=ref(''),selected=ref(''),busy=ref(false),error=ref('')
let generation=0
watch(()=>[session.unlocked,session.vaultEpoch],async()=>{
  const request=++generation;templates.value=[];name.value='';selected.value='';error.value=''
  if(!session.unlocked)return
  try{const result=await readPreference<Templates>('compose-templates',()=>({id:'compose-templates',templates:[]}));if(request===generation&&session.unlocked)templates.value=result.templates||[]}
  catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not load templates.'}
},{immediate:true})
onUnmounted(()=>{generation++;templates.value=[];name.value=''})
async function save(remove=false){
  if(busy.value||props.disabled||!session.unlocked)return
  const request=generation,id=selected.value||globalThis.crypto.randomUUID(),template={...props.draft,id,name:name.value.trim()}
  if(!remove&&(!template.name||template.name.length>128)){error.value='Enter a template name (maximum 128 characters).';return}
  busy.value=true;error.value=''
  try{
    const result=await updatePreference<Templates>('compose-templates',()=>({id:'compose-templates',templates:[]}),record=>{
      const remaining=(record.templates||[]).filter(item=>item.id!==id)
      const next=remove?remaining:[...remaining,template]
      if(next.length>20||new globalThis.TextEncoder().encode(JSON.stringify(next)).length>262144)throw new Error('Keep at most 20 templates totaling 256 KiB. Attachments and Bcc are not stored in templates.')
      record.templates=next
    })
    if(request===generation&&session.unlocked){templates.value=result.templates;selected.value=remove?'':id;name.value=remove?'':template.name}
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not save template.'}
  finally{busy.value=false}
}
function insert(){const value=templates.value.find(item=>item.id===selected.value);if(value&&!props.disabled&&!busy.value)emit('insert',{...value})}
</script>
<template>
  <details class="compose-templates"><summary>{{ t("Message templates") }}</summary>
    <fieldset :disabled="disabled||busy">
      <label>{{ t("Template") }}<select v-model="selected" @change="name=templates.find(item=>item.id===selected)?.name||''"><option value="">{{ t("New template") }}</option><option v-for="item in templates" :key="item.id" :value="item.id">{{item.name}}</option></select></label>
      <label>{{ t("Name") }}<input v-model="name" maxlength="128"></label>
      <button type="button" class="secondary" :disabled="!name.trim()" @click="save()">{{selected?t("Update from current message"):t("Save current message as template")}}</button>
      <button type="button" class="secondary" :disabled="!selected" @click="insert">{{ t("Insert into current message") }}</button>
      <button v-if="selected" type="button" class="text-button" @click="save(true)">{{ t("Delete template") }}</button>
      <p class="quiet">{{ t("Templates are encrypted in your vault. Insertion appends the body and fills empty recipient/subject fields. Attachments, Bcc and signatures are not stored.") }}</p>
    </fieldset><p v-if="error" class="error" role="alert">{{t(error)}}</p>
  </details>
</template>
