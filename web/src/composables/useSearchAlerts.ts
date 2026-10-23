
import { t } from '../i18n'
import {onUnmounted,ref,watch,type Ref} from 'vue'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
import {combinedSources,messageKey,type CombinedView} from '../api/mailSources'
import {listMessages,type FolderPage,type MessageFilters} from '../api/reading'
import type {SavedSearch} from '../api/preferences'

export function useSearchAlerts(searches:Ref<SavedSearch[]>,folders:()=>Record<string,FolderPage>){
 const session=useSessionStore(),mailbox=useMailboxStore(),notices=ref<string[]>([]),error=ref('')
 let controller=new globalThis.AbortController(),running=false
 const seen=new Map<string,Set<string>>()
 async function check(){
  if(running||!session.unlocked||globalThis.document.visibilityState==='hidden')return
  const signal=controller.signal;running=true;error.value=''
  try{
   for(const search of searches.value.filter(s=>s.alert).slice(0,3)){
    if(signal.aborted||!session.unlocked)return
    if(['tagged','snoozed','recent'].includes(search.view||'')){error.value='Search alerts do not evaluate tag, snooze or visit metadata.';continue}
    const selected=search.accountID&&search.scope!=='all'?[{accountID:search.accountID,folder:search.folder||'INBOX'}]:combinedSources(mailbox.accounts,folders(),(search.scope==='all'?'all':search.view||'inbox') as CombinedView,search.sources).sources
    if(selected.length>10){error.value='Alerts are limited to ten folders per search. Narrow the saved scope.';continue}
    const filters:MessageFilters={...search.filters,...(search.view==='unread'?{seen:'unread'}:{}),...(search.view==='flagged'?{flagged:'true'}:{})}
    const signature=JSON.stringify({...search,alert:true}),previous=seen.get(signature),next=new Set<string>();let added=0
    for(const source of selected){
     if(signal.aborted||!session.unlocked)return
     const page=await listMessages(source.accountID,signal,0,0,source.folder,filters)
     if(signal.aborted||!session.unlocked)return
     for(const message of page.messages){const key=messageKey({...source,validity:page.uid_validity,uid:message.uid});next.add(key);if(previous&&!previous.has(key))added++}
    }
    if(added)notices.value=[t("{0}: {1} newly matching messages in the checked windows.", {"0": search.name, "1": added}),...notices.value].slice(0,10)
    seen.set(signature,new Set([...(previous||[]),...next].slice(-1500)))
    for(const key of seen.keys())if(!searches.value.some(s=>JSON.stringify({...s,alert:true})===key))seen.delete(key)
   }
  }catch(value){if(!signal.aborted&&session.unlocked)error.value=value instanceof Error?value.message:'A saved-search alert could not be checked.'}finally{running=false}
 }
 watch(()=>[session.unlocked,session.vaultEpoch,session.userID],()=>{controller.abort();controller=new globalThis.AbortController();seen.clear();notices.value=[];error.value='';running=false})
 const timer=globalThis.setInterval(()=>{void check()},120000)
 watch(()=>searches.value.filter(s=>s.alert).map(s=>JSON.stringify(s)).join('|'),()=>{void check()})
 onUnmounted(()=>{controller.abort();globalThis.clearInterval(timer);seen.clear();notices.value=[]})
 return {notices,error,check}
}
