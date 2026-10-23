import type { Address, Message } from './reading'
import type { MailAction } from '../stores/mailActions'
import type { SMTPConfig } from './accounts'

// Select only a saved, authorized sender, never an arbitrary incoming address.
// Preserve exact aliases/plus-addressing; do not infer authorization by domain.
export function recipientReplySender(configs: SMTPConfig[], message: Pick<Message, 'to' | 'cc'>) {
  const available = configs.filter(config => config.sender_email?.trim())
  for (const recipient of [...message.to, ...message.cc]) {
    const email = recipient.email.trim().toLowerCase()
    const match = available.find(config => config.sender_email!.trim().toLowerCase() === email)
    if (match) return match
  }
  return available[0] || configs[0]
}

const escapeHTML = (value: string) => value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
export function actionDraft(action: MailAction, message: Message, ownEmails: string[]) {
  if(action==='draft')return {to:message.to.map(item=>item.email).join(', '),cc:message.cc.map(item=>item.email).join(', '),bcc:(message.bcc||[]).map(item=>item.email).join(', '),subject:message.subject,text:message.text,html:message.html,in_reply_to:message.in_reply_to}
  const own = new Set(ownEmails.map(email => email.trim().toLowerCase()))
  const unique = (input: Address[], exclude = new Set<string>()) => {
    const seen = new Set(exclude)
    return input.filter(address => { const email = address.email.trim().toLowerCase(); if (!email || own.has(email) || seen.has(email)) return false; seen.add(email); return true })
  }
  const reply = action === 'reply' || action === 'reply-all'
  let to = reply ? unique(message.reply_to?.length ? message.reply_to : message.from) : []
  if (reply && !to.length) to = unique(message.to)
  const cc = action === 'reply-all' ? unique([...message.to, ...message.cc],new Set(to.map(address => address.email.toLowerCase()))) : []
  const prefix = reply ? 'Re' : 'Fwd'
  const subject = action === 'redirect' ? message.subject : new RegExp(`^${reply ? 're' : 'fw(d)?'}:`, 'i').test(message.subject) ? message.subject : `${prefix}: ${message.subject}`
  const from = message.from.map(address => address.name ? `${address.name} <${address.email}>` : address.email).join(', ')
  const heading = `${reply ? 'On' : 'Forwarded message -'} ${message.date}, ${from} wrote:`
  const quote = message.html || `<p>${escapeHTML(message.text).replace(/\n/g, '<br>')}</p>`
  return { to: to.map(address => address.email).join(', '), cc: cc.map(address => address.email).join(', '), subject,
    text: action === 'redirect' ? '' : `\n\n${heading}\n${message.text.split('\n').map(line => `> ${line}`).join('\n')}`,
    html: action === 'redirect' ? '' : `<p></p><p>${escapeHTML(heading)}</p><blockquote>${quote}</blockquote>`,
    in_reply_to: reply && message.message_id ? `<${message.message_id.replace(/^<|>$/g, '')}>` : undefined,
  }
}
