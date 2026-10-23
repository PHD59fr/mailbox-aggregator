import {it,expect} from 'vitest'
import {parseAccountCSV,mergeCSV} from './accountCSV'
const header='name,imap_host,imap_port,imap_security,imap_username,imap_password'
it('parses quoted CSV and merges without replacing an existing password or default',()=>{
 const parsed=parseAccountCSV(header+'\n"Work, mailbox",imap.example.test,993,tls,alice,"pass,word"')
 expect(parsed.accounts[0]?.password).toBe('pass,word')
 const first=mergeCSV({accounts:{},smtp:{}},parsed,20);expect(Object.values(first.accounts)[0]?.is_default).toBe(true)
 const duplicate=parseAccountCSV(header+'\nNew name,imap.example.test,993,tls,alice,new-password')
 const merged=mergeCSV(first,duplicate,20);expect(merged.added).toBe(0);expect(merged.duplicates).toBe(1);expect(Object.values(merged.accounts)[0]?.password).toBe('pass,word')
})
it('rejects unknown headers, partial SMTP records, duplicate rows and limits before saving',()=>{
 expect(()=>parseAccountCSV(header+',unknown\nA,h,993,tls,u,p,x')).toThrow('header')
 expect(()=>parseAccountCSV(header+',smtp_host\nA,h,993,tls,u,p,smtp.example.test')).toThrow('SMTP')
 expect(()=>parseAccountCSV(header+'\nA,h,993,tls,u,p\nB,h,993,tls,u,p')).toThrow('duplicate')
 const parsed=parseAccountCSV(header+'\nA,h,993,tls,u,p');expect(()=>mergeCSV({accounts:{},smtp:{}},parsed,0)).toThrow('limit')
})
