import { describe, expect, it } from 'vitest'
import { combinedSources, messageKey, sourceKey } from './mailSources'
import type { Account } from './accounts'
import type { MailFolder } from './reading'
const account: Account = {id:'a',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true}
const folder = (name:string, role=''):MailFolder => ({name,role,delimiter:'/',selectable:true,messages:1,unseen:1,uid_next:2,uid_validity:7})
describe('combined mail sources',()=>{
  it('preserves the inbox default without requiring folder synchronization',()=>{
    expect(combinedSources([{...account,sync_folders:false}],{},'inbox')).toEqual({sources:[{accountID:'a',folder:'INBOX'}],notices:[]})
  })
  it('resolves roles per account, honors disabled mappings and avoids All Mail duplicates',()=>{
    const pages={a:{folders:[folder('Sent','sent'),folder('Custom'),folder('Everything','all'),folder('Junk','spam')],truncated:false}}
    expect(combinedSources([account],pages,'sent').sources).toEqual([{accountID:'a',folder:'Sent'}])
    expect(combinedSources([{...account,folder_mapping:{sent:'Custom'}}],pages,'sent').sources).toEqual([{accountID:'a',folder:'Custom'}])
    expect(combinedSources([{...account,folder_mapping:{sent:null}}],pages,'sent').sources).toEqual([])
    expect(combinedSources([account],pages,'all').sources.map(source=>source.folder)).toEqual(['Everything','Junk'])
  })
  it('reports partial discovery and missing chosen folders without silently broadening scope',()=>{
    const result=combinedSources([account],{a:{folders:[folder('Projects')],truncated:true}},'custom',[{accountID:'a',folder:'Missing'}])
    expect(result.sources).toEqual([]);expect(result.notices).toHaveLength(2)
    const capped=combinedSources([account],{a:{folders:Array.from({length:120},(_,i)=>folder(`Folder ${i}`)),truncated:false}},'all')
    expect(capped.sources).toHaveLength(100);expect(capped.notices.join(' ')).toContain('limited to 100')
  })
  it('separates identical UIDs between folders and delimiter-like account names',()=>{
    const ref={accountID:'a',uid:1,validity:7}
    expect(messageKey({...ref,folder:'Sent'})).not.toBe(messageKey({...ref,folder:'Drafts'}))
    expect(sourceKey({accountID:'a:b',folder:'c'})).not.toBe(sourceKey({accountID:'a',folder:'b:c'}))
  })
})
