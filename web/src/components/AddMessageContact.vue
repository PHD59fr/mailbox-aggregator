<script setup lang="ts">
import {t} from '../i18n'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { Address } from '../api/reading'
import { listContactBook, saveContact, type ContactBook } from '../api/preferences'
import { useSessionStore } from '../stores/session'
import AppIcon from './AppIcon.vue'

const props=defineProps<{addresses:Address[];iconOnly?:boolean}>()
const session=useSessionStore(),dialog=ref<InstanceType<typeof globalThis.HTMLDialogElement>|null>(null)
const opened=ref(false),busy=ref(false),loading=ref(false),error=ref(''),email=ref(''),name=ref(''),memberships=ref<string[]>([])
const book=ref<ContactBook>({contacts:[],groups:[]})
const choices=computed(()=>{
  const addresses=new Map<string,Address>()
  for(const address of props.addresses)if(/^[^\s<>@]+@[^\s<>@]+$/.test(address.email)&&!addresses.has(address.email.toLowerCase()))addresses.set(address.email.toLowerCase(),address)
  return [...addresses.values()]
})
const existing=computed(()=>book.value.contacts.find(contact=>contact.email.toLowerCase()===email.value.toLowerCase()))
let generation=0
function fill(){const contact=existing.value,address=choices.value.find(address=>address.email===email.value);name.value=contact?.name||address?.name||email.value.slice(0,128);memberships.value=[...(contact?.group_ids||[])]}
function close(){if(busy.value)return;generation++;dialog.value?.close();opened.value=false;name.value='';email.value='';memberships.value=[];book.value={contacts:[],groups:[]};error.value=''}
async function open(){
  if(!session.unlocked)return
  const request=++generation,epoch=session.vaultEpoch
  opened.value=true;loading.value=true;error.value='';email.value=choices.value[0]?.email||'';fill()
  await nextTick();dialog.value?.showModal()
  try{const result=await listContactBook();if(request!==generation||epoch!==session.vaultEpoch||!session.unlocked)return;book.value=result;fill()}
  catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not load your address book.'}
  finally{if(request===generation)loading.value=false}
}
async function save(){
  if(busy.value||loading.value||!session.unlocked)return
  const request=generation,epoch=session.vaultEpoch
  busy.value=true;error.value=''
  try{
    await saveContact({id:existing.value?.id||'',name:name.value,email:email.value,group_ids:[...memberships.value]})
    if(request!==generation||epoch!==session.vaultEpoch||!session.unlocked)return
    busy.value=false;close();session.showNotice('Contact saved in your address book.')
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not save this contact.'}
  finally{if(request===generation)busy.value=false}
}
function clear(){busy.value=false;close()}
watch([()=>session.vaultEpoch,()=>session.userID,()=>session.unlocked,()=>JSON.stringify(props.addresses)],clear)
onBeforeUnmount(clear)
</script>
<template>
  <button v-if="choices.length" type="button" :class="iconOnly?'address-contact-button':'secondary'" :aria-label="iconOnly?t('Add {0} to contacts',{'0':choices[0]?.email}):t('Add contact')" :title="iconOnly?t('Add {0} to contacts',{'0':choices[0]?.email}):undefined" :disabled="!session.unlocked" @click="open"><AppIcon :name="iconOnly?'contact-add':'contacts'" :size="15" /><span v-if="!iconOnly">{{ t("Add contact") }}</span></button>
  <Teleport to="body"><dialog v-if="opened" ref="dialog" class="message-contact-dialog" :aria-label="t('Save message contact')" @cancel.prevent="close" @click="($event.target === dialog) && close()">
    <form @submit.prevent="save"><header><h2>{{ existing ? t("Edit saved contact") : t("Add contact") }}</h2><button type="button" class="icon-button" :aria-label="t('Close new contact')" :disabled="busy" @click="close">×</button></header>
      <p v-if="loading" role="status">{{ t("Loading address book…") }}</p>
      <fieldset :disabled="busy||loading"><label>{{ t("Email address") }}<input v-if="choices.length===1" :value="email" type="email" readonly><select v-else v-model="email" @change="fill"><option v-for="address in choices" :key="address.email" :value="address.email">{{ address.name ? `${address.name} <${address.email}>` : address.email }}</option></select></label><label>{{ t("Contact name") }}<input v-model="name" autofocus required maxlength="128"></label>
        <fieldset v-if="book.groups.length"><legend>{{ t("Groups") }}</legend><label v-for="group in book.groups" :key="group.id" class="checkbox-option"><input v-model="memberships" type="checkbox" :value="group.id">{{ group.name }}</label></fieldset>
        <p v-if="existing" class="quiet">{{ t("This address is already saved. You can update its name and groups.") }}</p><p v-if="error" class="error" role="alert">{{ t(error) }}</p>
        <footer><button type="button" class="secondary" @click="close">{{ t("Cancel") }}</button><button type="submit" class="primary" :disabled="!name.trim()||!email">{{ busy?t("Saving…"):t("Save contact") }}</button></footer>
      </fieldset>
    </form>
  </dialog></Teleport>
</template>
<style scoped>
.address-contact-button { display:inline-flex;align-items:center;justify-content:center;flex:0 0 auto;vertical-align:middle;width:26px;height:26px;margin-inline-start:3px;padding:4px;border:0;border-radius:6px;background:transparent;color:var(--muted); }
.address-contact-button:hover, .address-contact-button:focus-visible { color:var(--accent);background:var(--raised); }
@media (pointer:coarse) { .address-contact-button { width:36px;height:36px; } }
.message-contact-dialog { width:min(480px,calc(100% - 28px));max-height:calc(100dvh - 28px);padding:24px;overflow:auto;color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:16px; } .message-contact-dialog::backdrop { background:#030712a6; } header,footer { display:flex;justify-content:space-between;align-items:center;gap:12px; } h2 { margin:0; } label { margin:16px 0; } footer { justify-content:flex-end;margin-top:20px; }
</style>
