import {onUnmounted,ref,watch} from 'vue'
import {readPreference} from '../api/preferences'
import {defaultShortcuts,type WorkspaceDisplay} from '../api/workspaceDisplay'
import {useMailboxStore} from '../stores/mailboxes'
import {useSessionStore} from '../stores/session'

// Applying a profile preference must not depend on displaying its settings form.
export function useWorkspaceDisplay() {
  const mailbox=useMailboxStore(),session=useSessionStore(),error=ref(''),loaded=ref(false)
  let generation=0
  watch(()=>[session.unlocked,session.vaultEpoch,session.userID],async()=>{
    const request=++generation,{vmk,userID}=session
    error.value='';loaded.value=false
    mailbox.display={...defaultShortcuts(),id:'workspace-display',density:'comfortable',layout:'split',colors:{}}
    if(!session.unlocked)return
    try {
      const record=await readPreference<WorkspaceDisplay>('workspace-display',()=>mailbox.display)
      if(request!==generation||!session.unlocked||session.vmk!==vmk||session.userID!==userID)return
      mailbox.display={...record,density:['comfortable','compact','spacious'].includes(record.density)?record.density:'comfortable',layout:record.layout==='below'?'below':'split',colors:record.colors||{},windowDays:[0,7,30,90,365].includes(record.windowDays||0)?record.windowDays||0:0,windowLimit:[0,50,100,250,500].includes(record.windowLimit||0)?record.windowLimit||0:0,shortcuts:{...defaultShortcuts().shortcuts,...record.shortcuts}}
      loaded.value=true
    } catch(value) {if(request===generation)error.value=value instanceof Error?value.message:'Could not load display settings.'}
  },{immediate:true})
  onUnmounted(()=>{generation++})
  return {error,loaded}
}
