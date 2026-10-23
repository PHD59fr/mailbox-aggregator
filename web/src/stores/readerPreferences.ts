import { defineStore } from 'pinia'
import { APIError } from '../api/client'
import { readCollection } from '../api/vaultCollection'

interface ReaderPreferences { id: string; block_external: boolean; addresses: string[]; domains: string[] }
const defaults = (): ReaderPreferences => ({ id: 'mail-reader', block_external: true, addresses: [], domains: [] })
export const useReaderPreferences = defineStore('reader-preferences', {
  state: () => ({ data: defaults(), loaded: false, saving: false, error: '', revision: '' as string | null }),
  actions: {
    clear() { this.data = defaults(); this.loaded = false; this.error = ''; this.revision = null },
    async load() {
      if (this.loaded) return
      try {
        const collection = await readCollection<ReaderPreferences>('preferences')
        const saved = collection.records['mail-reader']
        collection.assertUnlocked()
        this.data = { ...defaults(), ...saved, addresses: Array.isArray(saved?.addresses) ? saved.addresses : [], domains: Array.isArray(saved?.domains) ? saved.domains : [] }
        this.revision = collection.revision
        this.loaded = true; this.error = ''
      } catch (error) { this.data = defaults(); this.error = error instanceof Error ? error.message : 'Could not load privacy preferences.' }
    },
    allowed(address: string) {
      if (!this.loaded) return false
      const email = address.trim().toLowerCase(); const domain = email.split('@')[1]
      return !this.data.block_external || this.data.addresses.includes(email) || (!!domain && this.data.domains.includes(domain))
    },
    async update(change: (fresh: ReaderPreferences) => void) {
      if (this.saving) throw new Error('Wait for the current privacy update.')
      this.saving = true
      try {
        for (let attempt = 0; attempt < 3; attempt++) {
          const collection = await readCollection<ReaderPreferences>('preferences')
          const saved = collection.records['mail-reader']
          const next: ReaderPreferences = { ...defaults(), ...saved,
            addresses: Array.isArray(saved?.addresses) ? [...saved.addresses] : [],
            domains: Array.isArray(saved?.domains) ? [...saved.domains] : [] }
          change(next)
          collection.records['mail-reader'] = next
          try { await collection.save() }
          catch (error) {
            if (attempt < 2 && error instanceof APIError && error.code === 'REVISION_CONFLICT' && error.status === 409) continue
            throw error
          }
          collection.assertUnlocked()
          this.data = next; this.loaded = true; this.error = ''; this.revision = collection.revision
          return
        }
      } catch (error) { this.error = error instanceof Error ? error.message : 'Could not save preferences.'; throw error }
      finally { this.saving = false }
    },
    async save(next: Pick<ReaderPreferences, 'block_external'>) {
      await this.update(fresh => { fresh.block_external = next.block_external })
    },
    async trust(address: string, domainOnly: boolean) {
      const email = address.trim().toLowerCase(); const domain = email.split('@')[1]
      if (!email.includes('@') || !domain) throw new Error('This sender has no valid email address.')
      await this.update(fresh => {
        const list = domainOnly ? fresh.domains : fresh.addresses
        const value = domainOnly ? domain : email
        if (!list.includes(value)) list.push(value)
      })
    },
    async remove(value: string, domainOnly: boolean) {
      await this.update(fresh => {
        if (domainOnly) fresh.domains = fresh.domains.filter(item => item !== value)
        else fresh.addresses = fresh.addresses.filter(item => item !== value)
      })
    },
  },
})
