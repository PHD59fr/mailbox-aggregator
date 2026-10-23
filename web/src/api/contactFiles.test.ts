import { describe, expect, it } from 'vitest'
import { decodeContactFile, exportContactFile, parseContactFile, type ContactFormat } from './contactFiles'

const book={contacts:[{id:'a',name:'Élodie "Martin", team',email:'elodie@example.test',group_ids:['work','family']},{id:'b',name:'李明',email:'li@example.test'}],groups:[{id:'work',name:'Work'},{id:'family',name:'Famille, amis'}]}
describe('contact file interoperability',()=>{
  it.each<ContactFormat>(['outlook','google','thunderbird','vcard'])('round trips Unicode names, quoted fields, addresses and groups with %s',format=>{
    const file=exportContactFile(book,format),parsed=parseContactFile(file.text)
    expect(parsed.skipped).toBe(0)
    expect(parsed.entries).toEqual([{name:book.contacts[0]!.name,email:'elodie@example.test',groups:['Work','Famille, amis']},{name:'李明',email:'li@example.test',groups:[]}])
  })
  it('reads Google legacy multi-address CSV and labels without duplicating system groups',()=>{
    const parsed=parseContactFile('Name,E-mail 1 - Value,E-mail 2 - Value,Group Membership\r\nAlice,a@example.test,alias@example.test,* myContacts ::: Work ::: Family\r\nNo email,,,\r\nBad,invalid,,\r\n')
    expect(parsed.entries).toEqual([{name:'Alice',email:'a@example.test',groups:['Work','Family']},{name:'Alice',email:'alias@example.test',groups:['Work','Family']}]);expect(parsed.skipped).toBe(2)
  })
  it('reads localized Outlook semicolon CSV and Thunderbird tab-separated fields',()=>{
    expect(parseContactFile('Prénom;Nom;Adresse de messagerie\r\nÉlodie;Martin;elodie@example.test\r\n').entries[0]).toEqual({name:'Élodie Martin',email:'elodie@example.test',groups:[]})
    expect(parseContactFile('Display Name\tPrimary Email\tSecondary Email\nTeam\ta@example.test\tb@example.test').entries).toHaveLength(2)
  })
  it('handles multiline quoted CSV fields and fails an unfinished record before importing',()=>{
    expect(parseContactFile('Name,Email\r\n"Team\nEast",team@example.test\r\n').entries[0]!.name).toBe('Team\nEast')
    expect(()=>parseContactFile('Name,Email\n"unfinished,a@example.test')).toThrow('unfinished')
    expect(()=>parseContactFile('Name,Phone\nA,123')).toThrow('No email column')
  })
  it('reads folded vCard 4.0, escaped categories and multiple email addresses',()=>{
    const text='BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Élodie\r\n Martin\r\nEMAIL;TYPE=work:a@example.test\r\nitem1.EMAIL:b@example.test\r\nCATEGORIES:Work\\, team,Family\r\nEND:VCARD\r\n'
    expect(parseContactFile(text).entries).toEqual(['a@example.test','b@example.test'].map(email=>({name:'ÉlodieMartin',email,groups:['Work, team','Family']})))
    expect(()=>parseContactFile('BEGIN:VCARD\nFN:Incomplete')).toThrow('incomplete')
  })
  it('reads vCard 2.1 quoted-printable names and structured names',()=>{
    expect(parseContactFile('BEGIN:VCARD\nVERSION:2.1\nFN;CHARSET=UTF-8;ENCODING=QUOTED-PRINTABLE:=C3=89lo=\ndie Martin\nEMAIL:elodie@example.test\nEND:VCARD').entries[0]!.name).toBe('Élodie Martin')
    expect(parseContactFile('BEGIN:VCARD\nVERSION:3.0\nN:Martin;Élodie;;;\nEMAIL:elodie@example.test\nEND:VCARD').entries[0]!.name).toBe('Élodie Martin')
  })
  it('decodes UTF-8, old Western Windows CSV and UTF-16 exports',()=>{
    expect(decodeContactFile(new TextEncoder().encode('Élodie'))).toBe('Élodie')
    expect(decodeContactFile(Uint8Array.of(0xc9,108,111,100,105,101))).toBe('Élodie')
    expect(decodeContactFile(Uint8Array.of(255,254,0xc9,0,108,0))).toBe('Él')
  })
  it('folds vCard lines without splitting Unicode or injecting extra properties',()=>{
    const file=exportContactFile({contacts:[{id:'x',name:'é'.repeat(60)+'\nEMAIL:injected@example.test',email:'real@example.test'}],groups:[]},'vcard')
    for(const line of file.text.split('\r\n'))expect(new TextEncoder().encode(line).length).toBeLessThanOrEqual(75)
    expect(parseContactFile(file.text).entries[0]!.email).toBe('real@example.test')
  })
})
