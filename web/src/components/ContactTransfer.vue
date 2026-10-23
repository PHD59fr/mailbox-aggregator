<script setup lang="ts">
import { t } from '../i18n'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { importContacts, type ContactBook, type ContactImport } from '../api/preferences'
import { decodeContactFile, exportContactFile, maxContactFileBytes, parseContactFile, type ContactFormat } from '../api/contactFiles'
import { useSessionStore } from '../stores/session'

const props=defineProps<{book:ContactBook;disabled?:boolean}>()
const emit=defineEmits<{imported:[]}>()
const session=useSessionStore()
const dialog=ref<InstanceType<typeof globalThis.HTMLDialogElement>|null>(null)
const mode=ref<'import'|'export'|null>(null),busy=ref(false),error=ref('')
const entries=ref<ContactImport[]>([]),skipped=ref(0),filename=ref(''),format=ref<ContactFormat>('outlook')
let generation=0
const preview=computed(()=>{
  const emails=new Set(props.book.contacts.map(contact=>contact.email.toLowerCase()))
  let added=0,duplicates=0
  for(const entry of entries.value){const email=entry.email.toLowerCase();if(emails.has(email))duplicates++;else{emails.add(email);added++}}
  return {added,duplicates}
})
function close(){if(busy.value)return;generation++;dialog.value?.close();mode.value=null;entries.value=[];filename.value='';error.value='';skipped.value=0}
async function open(value:'import'|'export'){close();mode.value=value;await nextTick();dialog.value?.showModal()}
async function chooseFile(event:{target:unknown}){
  const input=event.target as InstanceType<typeof globalThis.HTMLInputElement>,file=input.files?.[0];input.value=''
  if(!file)return
  const request=++generation,epoch=session.vaultEpoch
  busy.value=true;error.value='';entries.value=[];filename.value=''
  try{
    if(file.size>maxContactFileBytes)throw new Error('Choose a contact file smaller than 5 MiB.')
    const bytes=new Uint8Array(await file.arrayBuffer())
    if(request!==generation||!session.unlocked||session.vaultEpoch!==epoch)return
    const parsed=parseContactFile(decodeContactFile(bytes))
    if(!parsed.entries.length)throw new Error('No valid email contacts found. Check the file format and email columns.')
    entries.value=parsed.entries;skipped.value=parsed.skipped;filename.value=file.name
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not read the contact file.'}
  finally{if(request===generation)busy.value=false}
}
async function importFile(){
  if(busy.value||!session.unlocked||!entries.value.length)return
  const request=generation,epoch=session.vaultEpoch
  busy.value=true;error.value=''
  try{
    const result=await importContacts(entries.value)
    if(request!==generation||!session.unlocked||session.vaultEpoch!==epoch)return
    busy.value=false;close();emit('imported');session.showNotice(t("{0} contacts added; {1} existing or repeated addresses kept.", {"0": result.added, "1": result.duplicates}))
  }catch(value){if(request===generation)error.value=value instanceof Error?value.message:'Could not import contacts.'}
  finally{if(request===generation)busy.value=false}
}
function download(){
  if(!session.unlocked||!props.book.contacts.length)return
  const file=exportContactFile(props.book,format.value),url=globalThis.URL.createObjectURL(new globalThis.Blob([file.text],{type:file.type}))
  const anchor=globalThis.document.createElement('a');anchor.href=url;anchor.download=file.filename;anchor.click()
  globalThis.setTimeout(()=>globalThis.URL.revokeObjectURL(url),1000);close()
}
function clear(){busy.value=false;close()}
watch(()=>session.vaultEpoch,clear)
onBeforeUnmount(clear)
</script>
<template>
  <div class="contact-transfer-actions"><button type="button" class="secondary" :disabled="disabled" @click="open('import')">{{ t("Import contacts") }}</button><button type="button" class="secondary" :disabled="disabled || !book.contacts.length" @click="open('export')">{{ t("Export contacts") }}</button></div>
  <Teleport to="body"><dialog v-if="mode" ref="dialog" class="contact-transfer-dialog" :aria-label="t('Contact import and export')" @cancel.prevent="close" @click="($event.target === dialog) && close()">
    <section><header><h2>{{ mode === 'import' ? t("Import contacts") : t("Export contacts") }}</h2><button type="button" class="icon-button" :aria-label="t('Close contact transfer')" :disabled="busy" @click="close">×</button></header>
      <template v-if="mode === 'import'">
        <p>{{ t("Choose a CSV export from Outlook, Google Contacts or Thunderbird, or a vCard (.vcf) file. Names, email addresses and groups are supported; phone numbers, photos and postal addresses are not imported.") }}</p>
        <label>{{ t("Contact file (maximum 5 MiB)") }}<input type="file" accept=".csv,.vcf,text/csv,text/vcard,text/x-vcard" :disabled="busy" @change="chooseFile"></label>
        <p v-if="busy" role="status">{{ filename ? t("Saving contacts…") : t("Reading contact file…") }}</p>
        <div v-if="entries.length" class="contact-import-preview"><p><strong>{{ filename }}</strong>: {{ preview.added }} {{ t("new contacts,") }} {{ preview.duplicates }} {{ t("existing or repeated addresses,") }} {{ skipped }} {{ t("skipped entries.") }}</p><p>{{ t("Existing names are preserved and group memberships are merged. Nothing is deleted.") }}</p><ul :aria-label="t('Import preview')"><li v-for="(entry,index) in entries.slice(0,5)" :key="index">{{ entry.name }} · {{ entry.email }}</li></ul></div>
      </template>
      <template v-else>
        <p>{{ t("Export all") }} {{ book.contacts.length }} {{ t("contacts. The downloaded file contains names and email addresses in plain text.") }}</p>
        <label>{{ t("Format") }}<select v-model="format"><option value="outlook">{{ t("Outlook CSV (UTF-8)") }}</option><option value="google">{{ t("Google Contacts CSV (Gmail)") }}</option><option value="thunderbird">{{ t("Thunderbird CSV") }}</option><option value="vcard">{{ t("vCard 3.0 (.vcf)") }}</option></select></label>
        <p class="quiet">{{ t("Group names are included as labels or categories. Their interpretation depends on the importing application.") }}</p>
      </template>
      <p v-if="error" class="error" role="alert">{{ t(error) }}</p>
      <footer><button type="button" class="secondary" :disabled="busy" @click="close">{{ t("Cancel") }}</button><button v-if="mode==='import'" type="button" class="primary" :disabled="busy||!entries.length" @click="importFile">{{ t("Import and merge") }}</button><button v-else type="button" class="primary" @click="download">{{ t("Download contacts") }}</button></footer>
    </section>
  </dialog></Teleport>
</template>
<style scoped>
.contact-transfer-actions { display:flex;flex-wrap:wrap;gap:8px;margin-bottom:18px; }
.contact-transfer-dialog { width:min(580px,calc(100% - 28px));max-height:calc(100dvh - 28px);padding:24px;overflow:auto;color:var(--ink);background:var(--panel);border:1px solid var(--line);border-radius:16px; }
.contact-transfer-dialog::backdrop { background:#030712a6; }
header,footer { display:flex;align-items:center;justify-content:space-between;gap:12px; } header h2 { margin:0; } header { margin-bottom:18px; } footer { justify-content:flex-end;margin-top:20px; } label { margin:16px 0; } p,li { overflow-wrap:anywhere; } .contact-import-preview { margin:16px 0;font-size:.85rem; }
</style>
