
import { t } from '../i18n'
import type { Account } from './accounts'
import type { FolderPage, MessageRef } from './reading'
import { presentFolders } from './folders'

export type CombinedView = 'inbox' | 'unread' | 'sent' | 'drafts' | 'flagged' | 'all' | 'custom' | 'tagged' | 'snoozed' | 'recent'
export interface MailSource { accountID: string; folder: string; validity?:number }
export const combinedViews: Record<CombinedView, string> = {
  inbox: 'All inboxes', unread: 'Unread', sent: 'Sent', drafts: 'Drafts', flagged: 'Starred', all: 'All mail', custom: 'Selected folders', tagged:'Tagged', snoozed:'Snoozed',recent:'Recently opened',
}
export const sourceKey = (source: MailSource) => JSON.stringify([source.accountID, source.folder,source.validity||0])
export const messageKey = (ref: MessageRef) => JSON.stringify([ref.accountID, ref.folder || 'INBOX', ref.validity, ref.uid])

// All Mail already includes Inbox/Sent and labels on providers such as Gmail.
// Prefer that role for broad views instead of duplicating every label's mail.
export function combinedSources(accounts: Account[], pages: Record<string, FolderPage>, view: CombinedView, chosen: MailSource[] = []) {
  const sources: MailSource[] = []
  const notices: string[] = []
  for (const account of accounts) {
    if (view === 'inbox' || view === 'unread') {
      sources.push({ accountID: account.id, folder: 'INBOX' })
      continue
    }
    if (!account.sync_folders) {
      notices.push(t("{0}: enable folder synchronization in Settings to include its folders.", {"0": account.name}))
      if (view === 'all' || view === 'flagged') sources.push({ accountID: account.id, folder: 'INBOX' })
      continue
    }
    const page = pages[account.id]
    if (!page) { notices.push(t("{0}: waiting for the folder list.", {"0": account.name})); continue }
    if (page.truncated) notices.push(t("{0}: the provider folder list is limited to 200 folders.", {"0": account.name}))
    const folders = presentFolders(page.folders, account.folder_mapping).filter(folder => folder.selectable)
    let names: string[]
    if (view === 'sent' || view === 'drafts') {
      names = folders.filter(folder => folder.role === view).map(folder => folder.name)
      if (!names.length) notices.push(t("{0}: no {1} folder is available in its mapping.", {"0": account.name, "1": view === 'sent' ? 'Sent' : 'Drafts'}))
    } else if (view === 'custom') {
      const requested = chosen.filter(source => source.accountID === account.id)
      names = requested.filter(source => source.folder === 'INBOX' || folders.some(folder => folder.name === source.folder)).map(source => source.folder)
      if (names.length !== requested.length) notices.push(t("{0}: a selected folder no longer exists or is unavailable.", {"0": account.name}))
    } else {
      const all = folders.find(folder => folder.role === 'all')
      names = all ? [all.name, ...folders.filter(folder => folder.role === 'trash' || folder.role === 'spam').map(folder => folder.name)] : ['INBOX', ...folders.map(folder => folder.name)]
    }
    for (const folder of new Set(names)) sources.push({ accountID: account.id, folder })
  }
  const unique = [...new Map(sources.map(source => [sourceKey(source), source])).values()]
  if (unique.length > 100) notices.push('This view is limited to 100 folders. Select fewer folders for a complete search.')
  return { sources: unique.slice(0, 100), notices }
}
