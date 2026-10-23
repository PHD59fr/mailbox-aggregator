import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import ReaderPrivacySettings from './ReaderPrivacySettings.vue'
import {useReaderPreferences} from '../stores/readerPreferences'
import {readCollection} from '../api/vaultCollection'
vi.mock('../api/vaultCollection',()=>({readCollection:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());vi.mocked(readCollection).mockResolvedValue({revision:null,records:{},save:vi.fn().mockResolvedValue(undefined),assertUnlocked:vi.fn()})})
describe('individual sender permissions in Settings',()=>{
  it('displays wildcard domain rules and revokes a domain without removing exact senders or other domains',async()=>{
    const preferences=useReaderPreferences();preferences.loaded=true
    preferences.data.addresses=['team@example.test'];preferences.data.domains=['example.test','another.test']
    vi.mocked(readCollection).mockResolvedValue({revision:'fresh',records:{'mail-reader':JSON.parse(JSON.stringify(preferences.data))},save:vi.fn().mockResolvedValue(undefined),assertUnlocked:vi.fn()})
    const wrapper=mount(ReaderPrivacySettings);await flushPromises()
    expect(wrapper.get('[aria-label="Trusted domains"]').text()).toContain('*@example.test')
    await wrapper.get('[aria-label="Remove trust for *@example.test"]').trigger('click');await flushPromises()
    expect(preferences.data.domains).toEqual(['another.test']);expect(preferences.data.addresses).toEqual(['team@example.test'])
    expect(preferences.allowed('other@example.test')).toBe(false);expect(preferences.allowed('team@example.test')).toBe(true)
    expect(preferences.allowed('other@another.test')).toBe(true);wrapper.unmount()
  })
  it('removes one trusted sender without deleting another sender or unrelated domain permission',async()=>{
    const preferences=useReaderPreferences();preferences.loaded=true
    preferences.data.addresses=['team@example.test','other@example.test'];preferences.data.domains=['another.test']
    vi.mocked(readCollection).mockResolvedValue({revision:'fresh',records:{'mail-reader':JSON.parse(JSON.stringify(preferences.data))},save:vi.fn().mockResolvedValue(undefined),assertUnlocked:vi.fn()})
    const wrapper=mount(ReaderPrivacySettings);await flushPromises()
    await wrapper.get('[aria-label="Remove trust for team@example.test"]').trigger('click');await flushPromises()
    expect(preferences.data.addresses).toEqual(['other@example.test']);expect(preferences.data.domains).toEqual(['another.test'])
    expect(preferences.allowed('team@example.test')).toBe(false)
    expect(wrapper.find('[aria-label="Remove trust for team@example.test"]').exists()).toBe(false);wrapper.unmount()
  })
  it('keeps the saved permission if removal fails',async()=>{
    const preferences=useReaderPreferences();preferences.loaded=true;preferences.data.addresses=['team@example.test']
    vi.mocked(readCollection).mockResolvedValue({revision:null,records:{},save:vi.fn().mockRejectedValue(new Error('Conflict')),assertUnlocked:vi.fn()})
    const wrapper=mount(ReaderPrivacySettings);await flushPromises()
    await wrapper.get('[aria-label="Remove trust for team@example.test"]').trigger('click');await flushPromises()
    expect(preferences.data.addresses).toEqual(['team@example.test']);expect(wrapper.get('[role="alert"]').text()).toContain('Conflict');wrapper.unmount()
  })
})
