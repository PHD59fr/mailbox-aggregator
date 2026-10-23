<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { setMessageFlag, transferMessage, trashMessage, type MailFolder, type MessageRef } from '../api/reading'
import { useSessionStore } from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
const props=defineProps<{selection: MessageRef[];folders: MailFolder[];scope?:string;foldersByAccount?:Record<string,MailFolder[]>}>()
const emit=defineEmits<{changed:[]}>()
const session=useSessionStore()
const mailboxes=useMailboxStore();const destinationAccount=ref('')
const action=ref('read');const destination=ref('');const busy=ref(false);const report=ref('')
let controller=new globalThis.AbortController()
const transferring=computed(()=>action.value==='move'||action.value==='copy')
const oneAccount=computed(()=>new Set(props.selection.map(item=>item.accountID)).size===1)
const destinations=computed(()=>(destinationAccount.value&&destinationAccount.value!==props.selection[0]?.accountID?props.foldersByAccount?.[destinationAccount.value]||[]:props.folders).filter(item=>item.selectable && (destinationAccount.value&&destinationAccount.value!==props.selection[0]?.accountID||item.name!==(props.selection[0]?.folder||'INBOX'))))
watch(destinationAccount,()=>{destination.value=''})
watch(()=>session.vaultEpoch,()=>controller.abort())
watch(()=>props.scope,()=>controller.abort())
onUnmounted(()=>controller.abort())
async function apply(){
 if(busy.value||!session.unlocked||!props.selection.length||props.selection.length>50||transferring.value&&(!oneAccount.value||!destination.value))return
 const references=props.selection.map(item=>({...item}));const chosen=action.value;const target=destination.value;const account=destinationAccount.value
 controller.abort();controller=new globalThis.AbortController();busy.value=true;report.value=''
 let completed=0;let failure=''
 try{
  for(const reference of references){
   if(controller.signal.aborted)throw new Error('The operation was cancelled. Refresh to check the last message.')
   if(chosen==='trash')await trashMessage(reference,controller.signal)
   else if(chosen==='copy'||chosen==='move')await transferMessage(reference,target,chosen==='move',controller.signal,account||undefined)
   else{
    const flag=chosen==='read'||chosen==='unread'?'seen':chosen==='flagged'||chosen==='unflagged'?'flagged':'answered'
    const enabled=chosen==='read'||chosen==='flagged'||chosen==='answered'
    await setMessageFlag(reference,flag,enabled,controller.signal)
   }
   completed++
  }
 }catch(error){failure=error instanceof Error?error.message:'The last action could not be confirmed.'}
 finally{
  busy.value=false
  report.value=`${completed} of ${references.length} actions confirmed.${failure?` ${failure} Remaining messages were not processed. Refresh and check the destination before retrying.`:''}`
  emit('changed')
 }
}
</script>
<template>
 <div class="selection-actions">
  <form v-if="selection.length" @submit.prevent="apply"><fieldset :disabled="busy">
   <legend>{{selection.length}} selected (maximum 50)</legend>
   <label>Action<select v-model="action"><option value="read">Mark read</option><option value="unread">Mark unread</option><option value="flagged">Add star</option><option value="unflagged">Remove star</option><option value="answered">Mark answered</option><option value="unanswered">Mark unanswered</option><option value="copy">Copy to folder</option><option value="move">Move to folder</option><option value="trash">Move to Trash</option></select></label>
   <template v-if="transferring&&oneAccount"><label>Destination account<select v-model="destinationAccount"><option value="">Same account</option><option v-for="account in mailboxes.accounts.filter(item=>item.id!==selection[0]?.accountID)" :key="account.id" :value="account.id">{{account.name}}</option></select></label><label>Destination folder<select v-model="destination" required><option value="">Choose a folder</option><option v-for="item in destinations" :key="item.name" :value="item.name">{{item.name}}</option></select></label><p v-if="!destinations.length" class="quiet">Enable folder synchronization for the destination account in Settings.</p></template>
   <p v-if="transferring&&!oneAccount" class="quiet">Select messages from one account to copy or move them.</p>
   <button type="submit" :disabled="busy||selection.length>50||transferring&&(!oneAccount||!destination)">{{busy?'Applying…':'Apply to selected messages'}}</button>
  </fieldset></form>
  <p v-if="report" role="status">{{report}}</p>
 </div>
</template>
