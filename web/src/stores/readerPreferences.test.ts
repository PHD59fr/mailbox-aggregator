import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useReaderPreferences } from './readerPreferences'
import { APIError } from '../api/client'
import { readCollection } from '../api/vaultCollection'
vi.mock('../api/vaultCollection', () => ({ readCollection: vi.fn() }))
beforeEach(() => { vi.resetAllMocks(); setActivePinia(createPinia()) })
describe('encrypted external-image permissions', () => {
  it('defaults to blocked and trusts exact addresses/domains only', async () => {
    const preferences = useReaderPreferences()
    expect(preferences.allowed('team@example.test')).toBe(false)
    const records = {}; const save = vi.fn().mockResolvedValue(undefined)
    vi.mocked(readCollection).mockResolvedValue({ revision:null, records, save, assertUnlocked:vi.fn() })
    await preferences.load(); await preferences.trust('Team@Example.test',false)
    expect(preferences.allowed('team@example.test')).toBe(true);expect(preferences.allowed('other@example.test')).toBe(false)
    await preferences.trust('team@example.test',true)
    expect(preferences.allowed('other@example.test')).toBe(true);expect(preferences.allowed('other@sub.example.test')).toBe(false)
    expect(save).toHaveBeenCalledTimes(2);expect(readCollection).toHaveBeenCalledWith('preferences')
    await preferences.remove('example.test',true);expect(preferences.allowed('other@example.test')).toBe(false)
    preferences.clear();expect(preferences.allowed('team@example.test')).toBe(false)
  })
  it('does not grant access when encrypted persistence fails', async () => {
    const preferences=useReaderPreferences()
    vi.mocked(readCollection).mockResolvedValue({revision:null,records:{},save:vi.fn().mockRejectedValue(new Error('Conflict')),assertUnlocked:vi.fn()})
    await preferences.load();await expect(preferences.trust('team@example.test',false)).rejects.toThrow('Conflict')
    expect(preferences.allowed('team@example.test')).toBe(false)
  })
})

it('regression: stale tab must preserve a remote image permission revocation', async () => {
 const store=useReaderPreferences()
 const old={id:'mail-reader',block_external:true,addresses:['revoked@example.test'],domains:[]}
 const fresh={...old,addresses:[]}
 vi.mocked(readCollection).mockResolvedValueOnce({revision:'old',records:{'mail-reader':old},save:vi.fn(),assertUnlocked:vi.fn()})
 await store.load()
 const records={'mail-reader':fresh}
 vi.mocked(readCollection).mockResolvedValueOnce({revision:'fresh',records,save:vi.fn(),assertUnlocked:vi.fn()})
 await store.trust('new@example.test',false)
 expect(records['mail-reader'].addresses).toEqual(['new@example.test'])
})

it('reapplies only the intended trust mutation after a concurrent revision conflict',async()=>{
 const store=useReaderPreferences()
 const saved={id:'mail-reader',block_external:true,addresses:['revoked@example.test'],domains:[]}
 const first={revision:'one',records:{'mail-reader':saved},save:vi.fn().mockRejectedValue(new APIError('REVISION_CONFLICT','changed',409)),assertUnlocked:vi.fn()}
 const fresh={revision:'two',records:{'mail-reader':{...saved,addresses:[],domains:['other.test']}},save:vi.fn(),assertUnlocked:vi.fn()}
 vi.mocked(readCollection).mockResolvedValueOnce(first).mockResolvedValueOnce(fresh)
 await store.trust('new@example.test',false)
 expect(store.data.addresses).toEqual(['new@example.test']);expect(store.data.domains).toEqual(['other.test'])
 expect(readCollection).toHaveBeenCalledTimes(2)
})
