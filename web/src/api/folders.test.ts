import {describe,expect,it} from 'vitest'
import {presentFolders,validateFolderMapping} from './folders'
import type {MailFolder} from './reading'
const folder=(name:string,role='',special_use=false):MailFolder=>({name,role,special_use,delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:2,uid_validity:7})
describe('standard folder mapping',()=>{
  it('distinguishes None from Automatic and keeps disabled server folders accessible as ordinary folders',()=>{
    const folders=[folder('Trash','trash',true),folder('Sent','sent',true),folder('none')]
    const disabled=presentFolders(folders,{trash:null,sent:null})
    expect(disabled).toHaveLength(3)
    expect(disabled.every(item=>!item.role && item.icon==='folder')).toBe(true)
    expect(presentFolders(folders,{}).find(item=>item.name==='Trash')?.role).toBe('trash')
    expect(presentFolders(folders,{trash:'none'}).find(item=>item.role==='trash')?.name).toBe('none')
    expect(()=>validateFolderMapping({trash:null,sent:null,drafts:null})).not.toThrow()
  })
  it('maps provider roles to standard labels/icons without changing actual IMAP paths',()=>{
    const folders=presentFolders([folder('INBOX'),folder('INBOX/Papierkorb','trash'),folder('[Gmail]/Sent Mail','sent',true),folder('Projects'),folder('Brouillons','drafts'),folder('Outbox','outbox')])
    expect(folders.map(item=>item.label)).toEqual(['Sent','Drafts','Outbox','Trash','Projects'])
    expect(folders[0]).toMatchObject({name:'[Gmail]/Sent Mail',icon:'send'})
    expect(folders[3]).toMatchObject({name:'INBOX/Papierkorb',icon:'trash'})
  })
  it('prioritizes SPECIAL-USE over a same-role fallback folder and keeps unmapped folders accessible',()=>{
    const result=presentFolders([folder('Sent'),folder('Provider sent','sent',true),folder('Archive/2026')])
    expect(result.find(item=>item.label==='Sent')?.name).toBe('Provider sent')
    expect(result.find(item=>item.name==='Sent')?.label).toBe('Sent')
    expect(result.find(item=>item.name==='Archive/2026')?.label).toBe('Archive/2026')
  })
  it('gives manual choices priority over SPECIAL-USE and removes old/conflicting role assignments',()=>{
    const result=presentFolders([folder('Provider trash','trash',true),folder('Personal/Deleted'),folder('Provider sent','sent',true),folder('Saved mail')],{trash:'Provider sent',sent:'Saved mail'})
    expect(result.find(item=>item.role==='trash')).toMatchObject({name:'Provider sent',label:'Trash',icon:'trash'})
    expect(result.find(item=>item.role==='sent')).toMatchObject({name:'Saved mail',label:'Sent'})
    expect(result.find(item=>item.name==='Provider trash')?.role).toBe('')
    expect(result.find(item=>item.name==='Personal/Deleted')?.name).toBe('Personal/Deleted')
  })
  it('does not silently fall back if a manually chosen folder disappears',()=>{
    expect(presentFolders([folder('Trash','trash',true)],{trash:'Missing'}).some(item=>item.role==='trash')).toBe(false)
    expect(presentFolders([folder('Trash','trash',true)],{}).some(item=>item.role==='trash')).toBe(true)
  })
  it('rejects duplicate roles, Inbox destinations, unknown roles and unsafe/oversized paths',()=>{
    for(const mapping of [{trash:'Same',sent:'Same'},{trash:'inbox'},{trash:'Bad\r\nfolder'},{trash:'é'.repeat(513)},{unknown:'Folder'}]) expect(()=>validateFolderMapping(mapping)).toThrow()
    expect(()=>validateFolderMapping({trash:'Projects/été & deleted',sent:'Sent',drafts:''})).not.toThrow()
  })
})
