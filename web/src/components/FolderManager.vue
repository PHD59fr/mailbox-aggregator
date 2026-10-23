<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { manageFolder, type MailFolder } from '../api/reading'
import { useSessionStore } from '../stores/session'
const props=defineProps<{accountID:string;folders:MailFolder[]}>()
const emit=defineEmits<{changed:[]}>()
const session=useSessionStore();const action=ref('create');const name=ref('');const newName=ref('');const confirmation=ref('');const busy=ref(false);const error=ref('');const success=ref('')
let controller=new globalThis.AbortController()
watch([()=>props.accountID,()=>session.vaultEpoch],()=>{controller.abort();name.value='';newName.value='';confirmation.value='';error.value='';success.value=''})
onUnmounted(()=>controller.abort())
async function submit(){
 if(busy.value||!session.unlocked||!name.value||action.value==='delete'&&confirmation.value!==name.value)return
 const epoch=session.vaultEpoch
 controller.abort();controller=new globalThis.AbortController();busy.value=true;error.value='';success.value=''
 try{
  await manageFolder(props.accountID,action.value,name.value,newName.value,confirmation.value,controller.signal)
  if(epoch!==session.vaultEpoch)return
  success.value='Folder action confirmed. Folder mappings may need updating in Settings.';name.value='';newName.value='';confirmation.value='';emit('changed')
 }catch(value){if(epoch===session.vaultEpoch)error.value=value instanceof Error?value.message:'Could not confirm the folder action. Refresh before retrying.'}
 finally{busy.value=false}
}
</script>
<template>
 <details class="folder-manager"><summary>Manage folders</summary><form @submit.prevent="submit"><fieldset :disabled="busy">
  <label>Folder action<select v-model="action" @change="confirmation='' "><option value="create">Create</option><option value="rename">Rename</option><option value="subscribe">Subscribe</option><option value="unsubscribe">Unsubscribe</option><option value="delete">Delete folder</option></select></label>
  <label v-if="action==='create'">Folder name<input v-model="name" required maxlength="1024"></label>
  <label v-else>Folder<select v-model="name" required><option value="">Choose a folder</option><option v-for="item in folders" :key="item.name" :value="item.name" :disabled="(action==='delete'||action==='rename')&&item.name.toUpperCase()==='INBOX'">{{item.name}}</option></select></label>
  <label v-if="action==='rename'">New name<input v-model="newName" required maxlength="1024"></label>
  <template v-if="action==='delete'"><p class="error">Deleting a folder can permanently delete its messages on the provider.</p><label>Type the exact folder name to confirm<input v-model="confirmation" autocomplete="off" required></label></template>
  <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="success" role="status">{{success}}</p>
  <button :disabled="busy||!name||action==='delete'&&confirmation!==name">{{busy?'Working…':'Apply folder action'}}</button>
 </fieldset></form></details>
</template>
