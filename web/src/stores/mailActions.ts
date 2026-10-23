import { defineStore } from 'pinia'
import type { Message, MessageRef } from '../api/reading'
export type MailAction = 'reply' | 'reply-all' | 'forward' | 'forward-attachment' | 'redirect' | 'draft'
export interface PendingAction { action: MailAction; reference: MessageRef; message: Message; smtpConfigID?: string; ownEmails?: string[]; replyFromRecipient?: boolean }
export const useMailActions = defineStore('mail-actions', {
  state: () => ({ pending: null as PendingAction | null }),
  actions: { clear() { this.pending = null } },
})
