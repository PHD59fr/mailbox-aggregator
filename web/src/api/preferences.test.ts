import {beforeEach,expect,it,vi} from 'vitest'
import {readCollection} from './vaultCollection'
import {APIError} from './client'
import {saveContact,deleteContact,saveSearch} from './preferences'
vi.mock('./vaultCollection',()=>({readCollection:vi.fn()}))
beforeEach(()=>vi.resetAllMocks())
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
