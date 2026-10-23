import { defineStore, storeToRefs } from 'pinia'
import { readCollection } from '../api/vaultCollection'
import { useSessionStore } from './session'

export const themes=[{id:'slate',name:'Slate',description:'Muted blue gray'},{id:'paper',name:'Paper',description:'Light, warm neutrals'},{id:'groove',name:'Groove',description:'Charcoal and green'},{id:'cinema',name:'Cinema',description:'Black and red'},{id:'system',name:'System',description:'Follow device appearance'}] as const
export type Theme=typeof themes[number]['id']
interface Appearance {id:string;theme:Theme}
const valid=(value:unknown):value is Theme=>themes.some(theme=>theme.id===value)
const migrate=(value:unknown)=>value==='spotify' ? 'groove' : value==='netflix' ? 'cinema' : value
const loads=new WeakMap<object,{generation:number;promise:Promise<void>}>()

function apply(value:Theme) {
  const root=document.documentElement
  root.dataset.themePreference=value
  root.dataset.theme=value==='system' ? (globalThis.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true) ? 'slate' : 'paper' : value
}

export const useThemeStore=defineStore('appearance',{
  state:()=>({selection:'slate' as Theme,loaded:false,saving:false,error:'',ownerID:'',generation:0}),
  actions:{
    clear(){this.generation++;loads.delete(this);this.selection='slate';this.loaded=false;this.saving=false;this.error='';this.ownerID='';apply('slate')},
    async load(){
      const session=useSessionStore()
      if(!session.authenticated || !session.unlocked){this.clear();return}
      if(this.ownerID && this.ownerID!==session.userID)this.clear()
      if(this.loaded)return
      const previous=loads.get(this)
      if(previous?.generation===this.generation)return previous.promise
      const {userID,vmk,vaultEpoch}=session;const generation=this.generation
      this.ownerID=userID;this.error=''
      const active=()=>session.authenticated && session.userID===userID && session.vmk===vmk && session.vaultEpoch===vaultEpoch && this.generation===generation && this.ownerID===userID
      const promise=(async()=>{
        try{
          const collection=await readCollection<Appearance>('preferences');collection.assertUnlocked()
          if(!active())return
          const value=migrate(collection.records.appearance?.theme)
          this.selection=valid(value)?value:'slate';this.loaded=true;apply(this.selection)
        }catch(error){if(active()){this.selection='slate';apply('slate');this.error=error instanceof Error ? error.message : 'Could not load your appearance preferences.'}}
      })()
      loads.set(this,{generation,promise})
      try{await promise}finally{if(loads.get(this)?.promise===promise)loads.delete(this)}
    },
    async setTheme(input:string){
      const value=migrate(input)
      if(!valid(value) || this.saving)return
      const session=useSessionStore()
      if(!this.loaded || !session.authenticated || !session.unlocked || this.ownerID!==session.userID){this.error='Unlock your profile and load its preferences before changing the theme.';return}
      const {userID,vmk,vaultEpoch}=session;const generation=this.generation;const previous=this.selection
      const active=()=>session.authenticated && session.userID===userID && session.vmk===vmk && session.vaultEpoch===vaultEpoch && this.generation===generation && this.ownerID===userID
      this.saving=true;this.error='';this.selection=value;apply(value)
      try{
        const collection=await readCollection<Appearance>('preferences');collection.assertUnlocked()
        if(!active())return
        collection.records.appearance={...collection.records.appearance,id:'appearance',theme:value}
        await collection.save();collection.assertUnlocked()
      }catch(error){
        if(active()){this.selection=previous;apply(previous);this.error=error instanceof Error ? error.message : 'Could not save your theme.'}
      }finally{if(active())this.saving=false}
    },
  },
})

export function initializeTheme(){
  apply('slate')
  // The old device-wide choice cannot be attributed to a specific profile.
  try{localStorage.removeItem('mailbox-theme')}catch{/* Obsolete browser preference. */}
  globalThis.matchMedia?.('(prefers-color-scheme: dark)').addEventListener('change',()=>{if(document.documentElement.dataset.themePreference==='system')apply('system')})
}

export function useTheme(){const store=useThemeStore();return {...storeToRefs(store),themes,setTheme:store.setTheme,load:store.load}}
