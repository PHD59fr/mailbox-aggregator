import { createPinia,setActivePinia } from 'pinia'
import { beforeEach,expect,it,vi } from 'vitest'
import { readPreference,updatePreference } from '../api/preferences'
import { useMailOrganization } from './organization'
import { useSessionStore } from './session'
import { messageKey } from '../api/mailSources'
vi.mock('../api/preferences',()=>({readPreference:vi.fn(),updatePreference:vi.fn()}))
const reference={accountID:'work',folder:'Archive',validity:7,uid:42}
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)})
  vi.mocked(readPreference).mockImplementation(async(_id,empty)=>empty())
  vi.mocked(updatePreference).mockImplementation(async(_id,empty,change)=>{const record=empty();change(record);return record})
})
it('stores only a stable reference, tags and an expiry through encrypted preferences',async()=>{
  const organization=useMailOrganization();await organization.load()
  await organization.set({...reference,folderID:'ephemeral-session-token'},{tags:['work','invoices'],snoozedUntil:Date.now()+60000})
  expect(organization.data.entries[messageKey(reference)]).toEqual({reference,tags:['work','invoices'],snoozedUntil:expect.any(Number)})
  expect(JSON.stringify(organization.data)).not.toContain('ephemeral-session-token')
  expect(organization.data.rememberRecent).toBe(false)
  await organization.remember(reference);expect(updatePreference).toHaveBeenCalledTimes(1)
})
it('does not repopulate references from a late read after the vault locks',async()=>{
  let finish!:(value:{id:string})=>void
  vi.mocked(readPreference).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}))
  const organization=useMailOrganization(),loading=organization.load()
  useSessionStore().lockLocal();organization.clear()
  finish({id:'mail-organization'});await loading
  expect(organization.loaded).toBe(false);expect(organization.data.entries).toEqual({})
})
