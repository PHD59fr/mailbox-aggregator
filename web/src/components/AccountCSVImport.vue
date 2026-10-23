<script setup lang="ts">
import {t} from '../i18n'
import {useRouter} from 'vue-router'
import {onUnmounted,ref,watch} from 'vue'
import {parseAccountCSV,mergeCSV,type CSVAccounts} from '../api/accountCSV'
import {listAccounts,listSMTPConfigs} from '../api/accounts'
import {importAccountCSV} from '../composables/useImportExport'
import {decodeContactFile} from '../api/contactFiles'
import {useSessionStore} from '../stores/session'
const router=useRouter(),session=useSessionStore(),input=ref<InstanceType<typeof globalThis.HTMLInputElement>|null>(null),preview=ref<CSVAccounts|null>(null),added=ref(0),duplicates=ref(0),confirmed=ref(false),busy=ref(false),error=ref('');let generation=0
function clear(){generation++;preview.value=null;confirmed.value=false;error.value='';added.value=0;duplicates.value=0;if(input.value)input.value.value=''}
watch(()=>[session.unlocked,session.userID,session.vaultEpoch],clear);onUnmounted(clear)
async function choose(){const file=input.value?.files?.[0];clear();if(!file||!session.unlocked)return;const g=generation;busy.value=true
 try{if(file.size>262144)throw new Error('Choose a CSV smaller than 256 KiB.');const bytes=new Uint8Array(await file.arrayBuffer());let parsed:CSVAccounts;try{parsed=parseAccountCSV(decodeContactFile(bytes))}finally{bytes.fill(0)}
 const [accounts,smtp]=await Promise.all([listAccounts(),listSMTPConfigs()]);if(g!==generation||!session.unlocked)return;const merged=mergeCSV({accounts,smtp},parsed,session.maxAccounts);preview.value=parsed;added.value=merged.added;duplicates.value=merged.duplicates}
 catch(value){if(g===generation)error.value=value instanceof Error?value.message:'Invalid CSV.'}finally{if(g===generation){busy.value=false;if(input.value)input.value.value=''}}}
async function save(){if(busy.value||!confirmed.value||!preview.value||!session.unlocked)return;const g=generation;busy.value=true;error.value=''
 try{await importAccountCSV(preview.value);if(!session.authenticated){await router.replace('/login');return}if(g===generation)clear()}
 catch(value){if(g===generation)error.value=value instanceof Error?value.message:'Import failed.'}finally{busy.value=false}}
</script>
<template><details><summary>{{ t("Import password accounts from CSV") }}</summary><p>{{ t("Up to 50 rows and 256 KiB. Preview and encryption run in this browser. Existing accounts are kept; duplicate receiving connections are skipped. A successful transaction signs out all web sessions, as with configuration import.") }}</p>
<p>{{ t("Required header:") }} <code>name,imap_host,imap_port,imap_security,imap_username,imap_password</code>{{ t(". Optional complete SMTP group:") }} <code>smtp_host,smtp_port,smtp_security,smtp_username,smtp_password,sender_email,sender_name</code>{{ t(". Security is") }} <code>tls</code> {{ t("or") }} <code>starttls</code>{{ t(". Passwords containing line breaks are rejected.") }}</p>
<label>{{ t("CSV file") }}<input ref="input" type="file" accept=".csv,text/csv" :disabled="busy" @change="choose" /></label>
<template v-if="preview"><p>{{added}} {{ t("new mailboxes;") }} {{duplicates}} {{ t("duplicate connections skipped. Password values are hidden.") }}</p><ul><li v-for="account in preview.accounts" :key="account.id">{{account.name}} — {{account.imap_username}} @ {{account.imap_host}}:{{account.imap_port}}{{account.smtp_config_id?t(" + linked SMTP"):''}}</li></ul><label><input v-model="confirmed" type="checkbox" :disabled="busy" />{{ t("I reviewed the accounts and accept signing out web sessions after the atomic import") }}</label><button type="button" :disabled="busy||!confirmed||!added" @click="save">{{ t("Encrypt and import accounts") }}</button><button type="button" class="secondary" :disabled="busy" @click="clear">{{ t("Discard preview") }}</button></template><p v-if="error" class="error" role="alert">{{t(error)}}</p></details></template>
