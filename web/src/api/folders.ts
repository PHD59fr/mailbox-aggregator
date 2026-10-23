import type { MailFolder } from './reading'
import type { FolderMapping, FolderRole } from './accounts'

export const folderRoles = {
  sent: {label:'Sent',icon:'send',order:1}, drafts:{label:'Drafts',icon:'edit',order:2}, outbox:{label:'Outbox',icon:'arrow',order:3},
  archive:{label:'Archive',icon:'folder',order:4}, all:{label:'All mail',icon:'mail',order:5}, spam:{label:'Spam',icon:'shield',order:6}, trash:{label:'Trash',icon:'trash',order:7},
} as const
export function validateFolderMapping(mapping: FolderMapping = {}) {
  const used = new Set<string>()
  for (const [role, name] of Object.entries(mapping)) {
    if (Object.prototype.hasOwnProperty.call(folderRoles, role) && name === null) continue
    if (!Object.prototype.hasOwnProperty.call(folderRoles, role) || typeof name !== 'string' || new TextEncoder().encode(name).length > 1024 || /[\r\n\0]/.test(name) || name.toUpperCase() === 'INBOX') throw new Error('Invalid folder mapping. Choose a server folder other than Inbox.')
    if (!name) continue
    if (used.has(name)) throw new Error('Choose a different server folder for each role.')
    used.add(name)
  }
}
function roleOf(folder:MailFolder) {
  // Compatibility with servers/builds that provide names but no SPECIAL-USE.
  const aliases:Record<string,string>={sent:'sent','sent items':'sent','sent mail':'sent',envoyés:'sent',drafts:'drafts',brouillons:'drafts',trash:'trash',corbeille:'trash',archive:'archive',archives:'archive',junk:'spam',spam:'spam',outbox:'outbox'}
  return folder.role || aliases[folder.name.toLowerCase()] || ''
}
export function presentFolders(folders:MailFolder[], mapping:FolderMapping = {}) {
  const manual = new Map(Object.entries(mapping).filter(([,name])=>!!name).map(([role,name])=>[name,role]))
  const effectiveRole = (folder:MailFolder) => {
    const assigned = manual.get(folder.name)
    if (assigned) return assigned
    const automatic = roleOf(folder)
    const override = mapping[automatic as FolderRole]
    return override === null || override ? '' : automatic
  }
  const primary=new Map<string,MailFolder>()
  for(const folder of folders) {
    const role=effectiveRole(folder)
    if(role && folder.selectable && (!primary.has(role) || folder.special_use)) primary.set(role,folder)
  }
  return folders.filter(folder=>folder.name.toUpperCase()!=='INBOX').map(folder=>{
    const role=effectiveRole(folder);const meta=primary.get(role)===folder ? folderRoles[role as FolderRole] : undefined
    return {...folder,role:meta ? role : '',label:meta?.label || folder.name,icon:meta?.icon || 'folder' as const,order:meta?.order || 20}
  }).sort((a,b)=>a.order-b.order || a.label.localeCompare(b.label))
}
