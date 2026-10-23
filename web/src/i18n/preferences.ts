import { defineStore } from 'pinia'
import { readPreference, updatePreference } from '../api/preferences'
import { useSessionStore } from '../stores/session'
import { direction, locale, setLanguage, type Direction, type Locale } from './index'

interface LanguagePreference {id:'interface-language';locale:Locale;direction:Direction}
const defaults = (): LanguagePreference => ({id:'interface-language',locale:locale.value,direction:direction.value})
export const useLanguagePreferences = defineStore('interface-language', {
  state: () => ({generation:0,loaded:false,busy:false,error:''}),
  actions: {
    clear() { this.generation++; this.loaded=false; this.busy=false; this.error='' },
    async load() {
      const session = useSessionStore(), {vmk,userID,vaultEpoch} = session, generation = this.generation
      if (!vmk || !session.unlocked) return
      try {
        const saved = await readPreference<LanguagePreference>('interface-language',defaults)
        if (generation!==this.generation || session.vmk!==vmk || session.userID!==userID || session.vaultEpoch!==vaultEpoch) return
        setLanguage(saved.locale,saved.direction); this.loaded=true
      } catch (value) { if (generation===this.generation) this.error=value instanceof Error?value.message:'Could not load language preferences.' }
    },
    async save(language:Locale,flow:Direction) {
      const session=useSessionStore(), {vmk,userID,vaultEpoch}=session, generation=this.generation
      if (!vmk || !session.unlocked || !this.loaded || this.busy || !['en','fr'].includes(language) || !['auto','ltr','rtl'].includes(flow)) return
      this.busy=true; this.error=''
      try {
        await updatePreference<LanguagePreference>('interface-language',defaults,record=>{record.locale=language;record.direction=flow})
        if (generation===this.generation && session.vmk===vmk && session.userID===userID && session.vaultEpoch===vaultEpoch) setLanguage(language,flow)
      } catch (value) { if (generation===this.generation) this.error=value instanceof Error?value.message:'Could not save language preferences.' }
      finally { if (generation===this.generation) this.busy=false }
    },
  },
})
