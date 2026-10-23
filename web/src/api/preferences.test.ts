import {beforeEach,expect,it,vi} from 'vitest'
import {readCollection} from './vaultCollection'
import {APIError} from './client'
import {saveContact,deleteContact,saveSearch,listContactBook,saveContactGroup,deleteContactGroup,importContacts} from './preferences'
vi.mock('./vaultCollection',()=>({readCollection:vi.fn()}))
beforeEach(()=>vi.resetAllMocks())
it('imports in one encrypted mutation, merging groups and preserving existing names and unrelated preferences',async()=>{
 const unrelated={id:'appearance',theme:'paper'}
 const records={'address-book':{id:'address-book',contacts:[{id:'a',name:'Keep my name',email:'a@example.test',group_ids:['work']}],groups:[{id:'work',name:'Work'}]},appearance:unrelated}
 const save=vi.fn().mockResolvedValue(undefined)
 vi.mocked(readCollection).mockResolvedValue({records,save,assertUnlocked:vi.fn(),revision:'one'})
 const result=await importContacts([{name:'Imported name',email:'A@example.test',groups:['Family']},{name:'B',email:'b@example.test',groups:['work']},{name:'Duplicate B',email:'B@example.test',groups:['Family']}])
 expect(result.added).toBe(1);expect(result.duplicates).toBe(2);expect(result.contacts).toHaveLength(2);expect(result.contacts[0]!.name).toBe('Keep my name')
 expect(result.groups.map(group=>group.name)).toEqual(['Work','Family']);expect(result.contacts.every(contact=>contact.group_ids?.length===2)).toBe(true)
 expect(save).toHaveBeenCalledTimes(1);expect(records.appearance).toBe(unrelated)
})
it('rejects an oversized import atomically without replacing existing contacts',async()=>{
 const existing={id:'a',name:'A',email:'a@example.test'}
 const records={'address-book':{id:'address-book',contacts:[existing]}};const save=vi.fn()
 vi.mocked(readCollection).mockResolvedValue({records,save,assertUnlocked:vi.fn(),revision:'one'})
 await expect(importContacts(Array.from({length:1000},(_,index)=>({name:'Contact',email:`person${index}@example.test`,groups:[]})))).rejects.toThrow('1000 contacts')
 expect(save).not.toHaveBeenCalled();expect(records['address-book'].contacts).toEqual([existing])
})
it('adds only an explicit contact and preserves other encrypted preferences after a conflict',async()=>{
 const save=vi.fn().mockRejectedValueOnce(new APIError('REVISION_CONFLICT','Conflict',409)).mockResolvedValueOnce(undefined)
 const first={id:'address-book',contacts:[{id:'a',name:'A',email:'a@example.test'}]}
 const fresh={id:'address-book',contacts:[...first.contacts,{id:'b',name:'B',email:'b@example.test'}]}
 const unrelated={id:'mail-reader',block_external:true}
 vi.mocked(readCollection).mockResolvedValueOnce({records:{'address-book':first},save,assertUnlocked:vi.fn(),revision:'old'}).mockResolvedValueOnce({records:{'address-book':fresh,'mail-reader':unrelated},save,assertUnlocked:vi.fn(),revision:'new'})
 const result=await saveContact({id:'c',name:'C',email:'c@example.test'})
 expect(result.map(item=>item.id)).toEqual(['a','b','c']);expect(unrelated.block_external).toBe(true)
 expect(save).toHaveBeenCalledTimes(2)
})
it('refuses duplicate addresses without writing and deletes only the requested contact',async()=>{
 const save=vi.fn().mockResolvedValue(undefined)
 const records={'address-book':{id:'address-book',contacts:[{id:'a',name:'A',email:'a@example.test'},{id:'b',name:'B',email:'b@example.test'}]}}
 vi.mocked(readCollection).mockResolvedValue({records,save,revision:'one',assertUnlocked:vi.fn()})
 await expect(saveContact({id:'c',name:'Another A',email:'A@example.test'})).rejects.toThrow('already exists');expect(save).not.toHaveBeenCalled()
 expect((await deleteContact('a')).map(item=>item.id)).toEqual(['b'])
})
it('stores search criteria without messages or results',async()=>{
 const records={};const save=vi.fn().mockResolvedValue(undefined)
 vi.mocked(readCollection).mockResolvedValue({records,save,revision:'one',assertUnlocked:vi.fn()})
 await saveSearch({id:'search',name:'Work unread',accountID:'work',folder:'INBOX',filters:{seen:'unread',from:'team'}})
 expect(records).toEqual({'saved-searches':{id:'saved-searches',searches:[{id:'search',name:'Work unread',accountID:'work',folder:'INBOX',filters:{seen:'unread',from:'team'}}]}})
})
it('reads existing address books without groups',async()=>{
 const contacts=[{id:'a',name:'Alex',email:'alex@example.test'}]
 const records={'address-book':{id:'address-book',contacts}}
 vi.mocked(readCollection).mockResolvedValue({records,save:vi.fn(),assertUnlocked:vi.fn(),revision:'one'})
 expect(await listContactBook()).toEqual({contacts,groups:[]})
})
it('preserves group membership when an older contact editor saves without groups',async()=>{
 const records={'address-book':{id:'address-book',contacts:[{id:'a',name:'A',email:'a@example.test',group_ids:['work']}],groups:[{id:'work',name:'Work'}]}}
 const save=vi.fn().mockResolvedValue(undefined)
 vi.mocked(readCollection).mockResolvedValue({records,save,assertUnlocked:vi.fn(),revision:'one'})
 expect((await saveContact({id:'a',name:'Alex',email:'a@example.test'}))[0]?.group_ids).toEqual(['work'])
 await expect(saveContact({id:'a',name:'Alex',email:'a@example.test',group_ids:['missing']})).rejects.toThrow('no longer exists')
 expect(save).toHaveBeenCalledTimes(1)
})
it('creates groups, rejects duplicate names and removes only the deleted membership',async()=>{
 const records={'address-book':{id:'address-book',contacts:[{id:'a',name:'A',email:'a@example.test',group_ids:['work','friends']}],groups:[{id:'work',name:'Work'},{id:'friends',name:'Friends'}]},appearance:{id:'appearance',theme:'paper'}}
 const save=vi.fn().mockResolvedValue(undefined)
 vi.mocked(readCollection).mockResolvedValue({records,save,assertUnlocked:vi.fn(),revision:'one'})
 await expect(saveContactGroup({id:'another',name:'WORK'})).rejects.toThrow('already exists')
 expect(save).not.toHaveBeenCalled()
 expect((await saveContactGroup({id:'family',name:'Family'})).groups.map(group=>group.id)).toEqual(['work','friends','family'])
 const result=await deleteContactGroup('work')
 expect(result.contacts).toEqual([{id:'a',name:'A',email:'a@example.test',group_ids:['friends']}])
 expect(result.groups.map(group=>group.id)).toEqual(['friends','family'])
 expect(records.appearance.theme).toBe('paper')
})
