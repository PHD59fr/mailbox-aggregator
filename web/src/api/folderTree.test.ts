import { describe, expect, it } from 'vitest'
import { buildFolderTree, folderAncestors } from './folderTree'
import type { MailFolder } from './reading'

const folder = (name:string,delimiter='/',selectable=true):MailFolder => ({name,delimiter,selectable,messages:1,unseen:1,uid_next:2,uid_validity:7})
describe('IMAP folder hierarchy',()=>{
  it('groups real parent folders with abbreviated child labels and keeps exact paths',()=>{
    const tree=buildFolderTree([folder('Loyer/nexim'),folder('Loyer'),folder('Loyer/adhoc'),folder('Loyers')])
    const parent=tree.find(node=>node.path==='Loyer')!
    expect(parent.folder?.selectable).toBe(true)
    expect(parent.children.map(node=>[node.label,node.path])).toEqual([['adhoc','Loyer/adhoc'],['nexim','Loyer/nexim']])
    expect(tree.some(node=>node.path==='Loyers')).toBe(true)
    expect(folderAncestors(tree,'Loyer/nexim')).toEqual(['Loyer'])
  })
  it('creates non-selectable missing parents and preserves non-selectable real containers',()=>{
    const tree=buildFolderTree([folder('Work/2026/July'),folder('Container','/',false),folder('Container/Child')])
    expect(tree.find(node=>node.path==='Work')?.folder).toBeUndefined()
    expect(tree.find(node=>node.path==='Container')?.folder?.selectable).toBe(false)
    expect(folderAncestors(tree,'Work/2026/July')).toEqual(['Work','Work/2026'])
    expect(folderAncestors(tree,'Other')).toBeNull()
  })
  it('uses each namespace’s server delimiter, not slashes in literal folder names',()=>{
    const tree=buildFolderTree([folder('Work.2026.July','.'),folder('Literal/Slash','')])
    expect(folderAncestors(tree,'Work.2026.July')).toEqual(['Work','Work.2026'])
    expect(tree.find(node=>node.path==='Literal/Slash')?.label).toBe('Literal/Slash')
    expect(tree.some(node=>node.path==='Literal')).toBe(false)
  })
  it('preserves standard roles inside the hierarchy and respects None',()=>{
    const folders=[{...folder('[Gmail]/Sent Mail'),role:'sent',special_use:true},folder('Work/Deleted')]
    const mapped=buildFolderTree(folders,{trash:'Work/Deleted',sent:null})
    expect(mapped.find(node=>node.path==='[Gmail]')?.hasStandardChildren).toBe(false)
    expect(mapped.find(node=>node.path==='[Gmail]')?.children[0]?.label).toBe('Sent Mail')
    expect(mapped.find(node=>node.path==='Work')?.children[0]?.label).toBe('Trash')
    expect(mapped.find(node=>node.path==='Work')?.hasStandardChildren).toBe(true)
  })
})
