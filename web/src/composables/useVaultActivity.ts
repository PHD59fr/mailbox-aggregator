import {onUnmounted,watch} from 'vue'
import {useSessionStore} from '../stores/session'
import {pruneTabKeys} from '../crypto/tabKeyStore'
import {flushActiveComposeDraft,pruneComposeDrafts,migrateComposeDrafts} from '../crypto/composeDraft'

export function useVaultActivity(){
  const session=useSessionStore();let timer:ReturnType<typeof setTimeout>|undefined
  function check(){
    clearTimeout(timer)
    if(!session.unlocked)return
    const remaining=session.lastActivity+session.idleMinutes*60000-Date.now()
    if(remaining<=0){void session.lock().catch(()=>{});return}
    timer=setTimeout(check,remaining)
  }
  const activity=()=>{if(!session.unlocked)return;if(Date.now()-session.lastActivity>=1000){session.activity();check()}}
  const events=['pointerdown','pointermove','keydown','scroll','touchstart'] as const
  for(const event of events)window.addEventListener(event,activity,{passive:true,capture:true})
  document.addEventListener('visibilitychange',check);window.addEventListener('pageshow',check)
  const channel=typeof window.BroadcastChannel==='function'?new window.BroadcastChannel('mailbox-vault-lock-v1'):null
  if(channel)channel.onmessage=event=>{if(event.data?.userID!==session.userID && !session.loading)return;const flushing=flushActiveComposeDraft();if(flushing)void flushing.then(()=>session.lockLocal(false),()=>{session.lockLocal(false);session.deferNotice('The latest draft changes could not be saved before locking. Browser storage may be unavailable or full.','warning')});else session.lockLocal(false)}
  const stop=watch([()=>session.unlocked,()=>session.idleMinutes,()=>session.resumeOnReload],()=>{check();if(session.unlocked){void session.remember();const vmk=session.vmk!;const userID=session.userID;void migrateComposeDrafts(vmk,userID,()=>session.vmk===vmk && session.userID===userID).catch(()=>{if(session.unlocked)session.showNotice('Some older draft attachments could not be migrated. Open those drafts and retry saving them.','warning')})}},{immediate:true})
  void pruneTabKeys().catch(()=>{})
  void pruneComposeDrafts()
  onUnmounted(()=>{stop();clearTimeout(timer);channel?.close();for(const event of events)window.removeEventListener(event,activity,true);document.removeEventListener('visibilitychange',check);window.removeEventListener('pageshow',check)})
}
