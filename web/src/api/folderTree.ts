import type { FolderMapping } from './accounts'
import type { MailFolder } from './reading'
import { presentFolders } from './folders'

type PresentedFolder = ReturnType<typeof presentFolders>[number]
export interface FolderNode {
  path: string
  label: string
  folder?: PresentedFolder
  children: FolderNode[]
  order: number
  hasStandardChildren: boolean
}

// The server delimiter, not a guessed slash, defines the hierarchy. Synthetic
// parents group missing LIST entries but can never be opened as mailboxes.
export function buildFolderTree(folders: MailFolder[], mapping: FolderMapping = {}): FolderNode[] {
  const roots: FolderNode[] = []
  const nodes = new Map<string, FolderNode>()
  for (const folder of presentFolders(folders, mapping)) {
    const parts = folder.delimiter ? folder.name.split(folder.delimiter) : [folder.name]
    let siblings = roots
    for (let index = 0; index < parts.length; index++) {
      const path = parts.slice(0, index + 1).join(folder.delimiter)
      let node = nodes.get(path)
      if (!node) {
        node = { path, label: parts[index] || folder.delimiter, children: [], order: 20, hasStandardChildren: false }
        nodes.set(path, node); siblings.push(node)
      }
      if (index === parts.length - 1) {
        node.folder = folder
        node.label = folder.role ? folder.label : parts[index] || folder.name
      }
      siblings = node.children
    }
  }
  function sort(branch: FolderNode[]) {
    for (const node of branch) {
      sort(node.children)
      node.order = Math.min(node.folder?.order ?? 20, ...node.children.map(child => child.order))
      node.hasStandardChildren = node.children.some(child => !!child.folder?.role || child.hasStandardChildren)
    }
    branch.sort((a,b) => a.order - b.order || a.label.localeCompare(b.label))
  }
  sort(roots)
  return roots
}

export function folderAncestors(nodes: FolderNode[], path: string): string[] | null {
  for (const node of nodes) {
    if (node.folder?.name === path) return []
    const children = folderAncestors(node.children, path)
    if (children) return [node.path, ...children]
  }
  return null
}
