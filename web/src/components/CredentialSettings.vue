<script setup lang="ts">
import {t} from '../i18n'
import { ref } from 'vue'
import { changePassword } from '../crypto/credentials'
import { useSessionStore } from '../stores/session'
import { useRouter } from 'vue-router'
const session=useSessionStore();const router=useRouter()
const login=ref(session.login);const current=ref('');const password=ref('');const confirmation=ref('');const rotate=ref(false);const busy=ref(false);const error=ref('')
async function submit(){
 if(busy.value||!session.unlocked)return
 if(password.value!==confirmation.value){error.value='Passwords do not match.';return}
 busy.value=true;error.value=''
 try{await changePassword(login.value,current.value,password.value,rotate.value);await router.replace('/login')}
 catch(value){error.value=value instanceof Error?value.message:'Credential change could not be confirmed. Sign in again before retrying.'}
 finally{current.value='';password.value='';confirmation.value='';busy.value=false}
}
</script>
<template>
 <section id="credentials"><h2>{{ t("Password and vault key") }}</h2><p>{{ t("Changing your password signs out all sessions and disables existing gateway access. Keep your new password safe; there is no administrator recovery.") }}</p>
 <form @submit.prevent="submit"><fieldset :disabled="busy">
 <label>{{ t("Login identifier") }}<input v-model="login" autocomplete="username" required></label>
 <label>{{ t("Current password") }}<input v-model="current" type="password" autocomplete="current-password" required></label>
 <label>{{ t("New password") }}<input v-model="password" type="password" autocomplete="new-password" minlength="12" required></label>
 <label>{{ t("Confirm new password") }}<input v-model="confirmation" type="password" autocomplete="new-password" required></label>
 <label class="checkbox-option"><input v-model="rotate" type="checkbox">{{ t("Also rotate the vault key and re-encrypt all configuration") }}</label>
 <p v-if="rotate" class="quiet">{{ t("Older encrypted backups remain tied to their original keys. Local browser drafts are migrated after unlocking on each device.") }}</p>
 <p v-if="error" class="error" role="alert">{{t(error)}}</p><button class="primary" :disabled="busy||!current||!password||password!==confirmation">{{busy?t("Updating credentials…"):t("Change password")}}</button>
 </fieldset></form></section>
</template>
