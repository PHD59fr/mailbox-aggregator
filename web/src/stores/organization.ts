import { defineStore } from 'pinia'
import { readPreference, updatePreference } from '../api/preferences'
import type { MessageRef } from '../api/reading'
import { messageKey } from '../api/mailSources'
import { useSessionStore } from './session'

export interface MessageMark { reference:MessageRef; tags:string[]; snoozedUntil?:number; visitedAt?:number }
interface Organization {id:'mail-organization'; entries:Record<string,MessageMark>;rememberRecent:boolean}
const writes=new WeakMap<object,Promise<void>>()
const empty=():Organization=>({id:'mail-organization',entries:{},rememberRecent:false})
function validRef(ref:MessageRef){return ref&&typeof ref.accountID==='string'&&ref.accountID.length<=128&&Number.isInteger(ref.uid)&&ref.uid>0&&ref.uid<=4294967295&&Number.isInteger(ref.validity)&&ref.validity>0&&ref.validity<=4294967295&&(!ref.folder||typeof ref.folder==='string'&&ref.folder.length<=1024&&!/[\r\n\0]/.test(ref.folder))}
function clean(record:Organization){
  const entries:Record<string,MessageMark>={}
  for(const mark of Object.values(record.entries||{}).slice(0,1000)){
    if(!mark||!validRef(mark.reference)||!Array.isArray(mark.tags))continue
    const {accountID,uid,validity,folder}=mark.reference
    const reference={accountID,uid,validity,...(folder?{folder}:{})}
    entries[messageKey(reference)]={reference,tags:mark.tags.filter(tag=>typeof tag==='string'&&tag.trim()&&tag.length<=64).slice(0,12),
      ...(Number.isFinite(mark.snoozedUntil)?{snoozedUntil:mark.snoozedUntil}:{}),...(Number.isFinite(mark.visitedAt)?{visitedAt:mark.visitedAt}:{})}
  }
  const recent=Object.entries(entries).filter(([,mark])=>mark.visitedAt).sort((a,b)=>b[1].visitedAt!-a[1].visitedAt!)
  for(const [index,[key,mark]] of recent.entries())if(index>=100||mark.visitedAt!<Date.now()-30*86400000||record.rememberRecent!==true){delete mark.visitedAt;if(!mark.tags.length&&!mark.snoozedUntil)delete entries[key]}
  return {id:'mail-organization' as const,entries,rememberRecent:record.rememberRecent===true}
}
export const useMailOrganization=defineStore('mail-organization',{
  state:()=>({data:empty(),loaded:false,error:'',generation:0}),
  actions:{
    clear(){this.generation++;this.data=empty();this.loaded=false;this.error=''},
    async load(){
      const session=useSessionStore(),request=++this.generation
      if(!session.unlocked){this.clear();return}
      const vmk=session.vmk,user=session.userID
      try{const record=await readPreference<Organization>('mail-organization',empty);if(request===this.generation&&session.vmk===vmk&&session.userID===user){this.data=clean(record);this.loaded=true;this.error=''}}
      catch(value){if(request===this.generation)this.error=value instanceof Error?value.message:'Could not load tags and reminders.'}
    },
    async change(edit:(record:Organization)=>void){
      const session=useSessionStore(),vmk=session.vmk,user=session.userID,request=this.generation
      if(!vmk||!this.loaded)throw new Error('Unlock the vault and load organization settings first.')
      const preceding=writes.get(this)||Promise.resolve()
      const pending=preceding.catch(()=>{}).then(async()=>{
        if(request!==this.generation||session.vmk!==vmk||session.userID!==user)throw new Error('The vault was locked before saving references.')
      const result=await updatePreference<Organization>('mail-organization',empty,record=>{
        const next=clean(record);edit(next)
        if(Object.keys(next.entries).length>1000||new globalThis.TextEncoder().encode(JSON.stringify(next)).length>524288)throw new Error('Keep at most 1000 message references totaling 512 KiB. Remove old references first.')
        Object.assign(record,next)
      })
      if(request===this.generation&&session.vmk===vmk&&session.userID===user)this.data=clean(result)
      })
      writes.set(this,pending)
      try{await pending}finally{if(writes.get(this)===pending)writes.delete(this)}
    },
    async set(reference:MessageRef,patch:Partial<Omit<MessageMark,'reference'>>){
      if(!validRef(reference))throw new Error('Invalid message reference.')
      const {accountID,uid,validity,folder}=reference,key=messageKey(reference)
      if(patch.tags&&(patch.tags.length>12||patch.tags.some(tag=>!tag.trim()||tag.length>64)))throw new Error('Use at most 12 tags of 64 characters each.')
      await this.change(record=>{const previous=record.entries[key]||{reference:{accountID,uid,validity,...(folder?{folder}:{})},tags:[]};record.entries[key]={...previous,...patch,...(patch.tags?{tags:[...new Set(patch.tags.map(tag=>tag.trim()))]}:{})}})
    },
    async remove(reference:MessageRef){await this.change(record=>{delete record.entries[messageKey(reference)]})},
    async remember(reference:MessageRef){
      if(!this.data.rememberRecent||!this.loaded)return
      await this.change(record=>{
        const key=messageKey(reference),{accountID,uid,validity,folder}=reference
        record.entries[key]={...(record.entries[key]||{reference:{accountID,uid,validity,...(folder?{folder}:{})},tags:[]}),visitedAt:Date.now()}
        const recent=Object.entries(record.entries).filter(([,mark])=>mark.visitedAt).sort((a,b)=>b[1].visitedAt!-a[1].visitedAt!)
        for(const [index,[oldKey,mark]] of recent.entries())if(index>=100||mark.visitedAt!<Date.now()-30*86400000){delete mark.visitedAt;if(!mark.tags.length&&!mark.snoozedUntil)delete record.entries[oldKey]}
      })
    },
  },
})
