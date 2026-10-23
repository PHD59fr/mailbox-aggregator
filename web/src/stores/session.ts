import { legacyVaultKeys } from '../crypto/keyHistory'
import { migrateComposeDrafts } from '../crypto/composeDraft'
import { cancelKeyDerivation } from '../crypto/argon2'
import { defineStore } from 'pinia'
import { api, APIError, setCSRFToken } from '../api/client'
import { clearKey, unlockVault, type VaultMetadata } from '../crypto/vault'
import type { AuthSession } from '../crypto/auth'
import {broadcastVaultLock,discardTabResume,restoreTabResume,saveTabResume,touchTabResume} from '../crypto/tabResume'
import {discardActiveComposeDraft,flushActiveComposeDraft} from '../crypto/composeDraft'

type SessionView=AuthSession & { idle_timeout_seconds?: number }
const noticeTimers = new WeakMap<object, ReturnType<typeof setTimeout>>()
const resumeWrites=new WeakMap<object,{epoch:number;vmk:Uint8Array;promise:Promise<void>}>()
function savedIdle(){try{const value=Number(localStorage.getItem('mailbox-idle-minutes'));return [1,5,10,15,30,60].includes(value)?value:15}catch{return 15}}
function savedResume(){try{return localStorage.getItem('mailbox-resume-enabled')!=='false'}catch{return true}}
export const useSessionStore = defineStore('session', { state: () => ({ authenticated: false, userID: '', login: '', csrf: '', vmk: null as Uint8Array | null, vaultEpoch: 0, loading: true, error: '', notice: '',noticeKind:'success' as 'success'|'warning',deferredNotice:'',deferredNoticeKind:'success' as 'success'|'warning',sentDraftID:'',pendingDraftIDs:[] as string[],serverLockRevision:0,serverIdleTimeoutSeconds:0,maxAccounts:20,lastActivity:Date.now(),idleMinutes:savedIdle(),resumeOnReload:savedResume(),resumeAvailable:false }), getters: { unlocked: state => state.vmk !== null, activationLifetimeSeconds: state => state.serverIdleTimeoutSeconds || state.idleMinutes * 60 }, actions: {
  showNotice(message: string,kind:'success'|'warning'='success') {
    this.dismissNotice(); this.notice = message;this.noticeKind=kind
    const timer = setTimeout(() => { if (this.notice === message) this.notice = ''; noticeTimers.delete(this) }, kind==='warning' ? 12000 : 6000)
    noticeTimers.set(this,timer)
  },
  dismissNotice() { clearTimeout(noticeTimers.get(this)); noticeTimers.delete(this); this.notice = '' },
  // An SMTP outcome can arrive after the composer is gone, including during a
  // vault lock. Keep it until the workspace is visibly available again.
  deferNotice(message: string,kind:'success'|'warning'='success') {
    if (this.unlocked) { this.showNotice(message,kind); return }
    this.deferredNotice = message;this.deferredNoticeKind = kind
  },
  flushDeferredNotice() {
    if (!this.deferredNotice) return
    const message = this.deferredNotice;const kind = this.deferredNoticeKind
    this.deferredNotice = ''
    this.showNotice(message,kind)
  },
  accept(view: SessionView, login?: string) { if (this.userID !== view.user_id || this.csrf !== view.csrf_token) { this.lockLocal(false);this.deferredNotice='';this.sentDraftID='';this.pendingDraftIDs=[] } this.authenticated = true; this.userID = view.user_id; if (login) this.login = login.trim().toLowerCase(); this.csrf = view.csrf_token;this.serverLockRevision=view.lock_revision??0;this.serverIdleTimeoutSeconds=view.idle_timeout_seconds??0;this.maxAccounts=view.max_accounts??20; setCSRFToken(view.csrf_token) },
  clearLocalSession() { this.lockLocal(); this.authenticated = false; this.userID = ''; this.login = ''; this.csrf = '';this.deferredNotice='';this.sentDraftID='';this.pendingDraftIDs=[];this.serverIdleTimeoutSeconds=0; setCSRFToken('') },
  async restore() {
    this.loading=true;const epoch=this.vaultEpoch
    let resumed:Awaited<ReturnType<typeof restoreTabResume>>=null
    try {
      const view=await api<SessionView>('/session')
      if(this.resumeOnReload) resumed=await restoreTabResume(view.user_id,view.csrf_token,view.lock_revision??0)
      if(epoch!==this.vaultEpoch){clearKey(resumed?.vmk || null);return}
      if(resumed) {
        const latest=await api<SessionView>('/session')
        if(epoch!==this.vaultEpoch || latest.user_id!==view.user_id || latest.csrf_token!==view.csrf_token || (latest.lock_revision??0)!==(view.lock_revision??0)) {clearKey(resumed.vmk);this.clearLocalSession();return}
      }
      this.accept(view)
      if(resumed) {this.vmk=resumed.vmk;this.lastActivity=resumed.lastActivity;this.idleMinutes=resumed.idleMinutes;await this.remember()}
    } catch {clearKey(resumed?.vmk || null);this.clearLocalSession()} finally {this.loading=false}
  },
  async remember() {
    const vmk=this.vmk;const epoch=this.vaultEpoch
    if(!vmk || !this.resumeOnReload)return
    const existing=resumeWrites.get(this);if(existing?.vmk===vmk && existing.epoch===epoch)return existing.promise
    const userID=this.userID;const csrf=this.csrf;const revision=this.serverLockRevision
    const active=()=>this.vmk===vmk && this.vaultEpoch===epoch && this.userID===userID && this.csrf===csrf && this.resumeOnReload
    const promise=(async()=>{const saved=await saveTabResume(vmk,userID,csrf,revision,this.lastActivity,this.idleMinutes,active);if(active()){this.resumeAvailable=saved;if(saved)touchTabResume(this.lastActivity,this.idleMinutes)}})()
    resumeWrites.set(this,{epoch,vmk,promise})
    try{await promise}finally{if(resumeWrites.get(this)?.promise===promise)resumeWrites.delete(this)}
  },
  setIdleMinutes(value:number) {if(![1,5,10,15,30,60].includes(value))return;this.idleMinutes=value;try{localStorage.setItem('mailbox-idle-minutes',String(value))}catch{/* Non-secret browser preference. */}touchTabResume(this.lastActivity,value)},
  async setResumeOnReload(value:boolean) {this.resumeOnReload=value;try{localStorage.setItem('mailbox-resume-enabled',String(value))}catch{/* Non-secret browser preference. */}if(value)await this.remember();else{resumeWrites.delete(this);discardTabResume();this.resumeAvailable=false}},
  activity() {if(!this.unlocked)return;this.lastActivity=Date.now();touchTabResume(this.lastActivity,this.idleMinutes)},
  async unlock(password: string) {
    if (!this.authenticated) throw new Error('Sign in before unlocking the vault.')
    const userID = this.userID
    const csrf = this.csrf
    const vaultEpoch = this.vaultEpoch
    let view:SessionView
    try{view=await api<SessionView>('/session')}catch(error){
      if(error instanceof APIError && error.status===401){this.clearLocalSession();throw new APIError('SESSION_COOKIE_UNAVAILABLE','Sign-in succeeded, but the browser could not keep your session cookie. Open the app using its HTTPS address and allow cookies, then sign in again. This is a session problem, not an incorrect password.',401,error)}
      throw error
    }
    if(view.user_id!==userID || view.csrf_token!==csrf || this.vaultEpoch!==vaultEpoch)throw new Error('The session changed. Sign in again.')
    const metadata = await api<VaultMetadata>('/vault/metadata')
    const vmk = await unlockVault(password, userID, metadata)
    if (!this.authenticated || this.userID !== userID || this.csrf !== csrf || this.vaultEpoch !== vaultEpoch) {
      clearKey(vmk)
      throw new Error('The session changed. Sign in again.')
    }
    let migrationFailed=false
    if (metadata.key_version > 1) {
      try {
        const oldKeys = await legacyVaultKeys(vmk,userID)
        try { await migrateComposeDrafts(vmk,userID,()=>this.vaultEpoch===vaultEpoch && this.userID===userID,oldKeys) }
        finally { for(const key of oldKeys)clearKey(key) }
      } catch {migrationFailed=true}
      if(!this.authenticated||this.userID!==userID||this.csrf!==csrf||this.vaultEpoch!==vaultEpoch){clearKey(vmk);throw new Error('The vault was locked while migrating drafts.')}
    }
    clearKey(this.vmk)
    this.vmk = vmk
    this.serverLockRevision=view.lock_revision??0;this.lastActivity=Date.now()
    this.error = ''
    this.flushDeferredNotice()
    if(migrationFailed)this.showNotice('Some local drafts could not be migrated. They have been retained; retry unlocking on this device before their seven-day expiry.','warning')
    await this.remember()
  },
  lockLocal(broadcast=true) { cancelKeyDerivation(); this.vaultEpoch++; clearKey(this.vmk); this.vmk = null; this.dismissNotice();resumeWrites.delete(this);discardTabResume();this.resumeAvailable=false;if(broadcast)broadcastVaultLock(this.userID) },
  async lock() {
    const flushing=flushActiveComposeDraft()
    let draftFailed=false
    if(flushing)try{await flushing}catch{draftFailed=true}
    this.lockLocal()
    if(draftFailed)this.deferNotice('The latest draft changes could not be saved before locking. Browser storage may be unavailable or full.','warning')
    await api<void>('/session/credentials', { method: 'DELETE' })
  },
  async logout() {
    const flushing=flushActiveComposeDraft()
    if(flushing)try{await flushing}catch{/* Logout explicitly discards the local draft. */}
    const discarding=discardActiveComposeDraft()
    if(discarding)await discarding
    this.lockLocal()
    try { await api<void>('/auth/logout', { method: 'POST' }) } finally { this.clearLocalSession() }
  },
} })
