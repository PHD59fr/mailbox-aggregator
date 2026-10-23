
import type { ContactBook, ContactImport } from './preferences'

export type ContactFormat = 'outlook' | 'google' | 'thunderbird' | 'vcard'
export const maxContactFileBytes = 5 * 1024 * 1024
const validEmail = (value: string) => value.length <= 254 && /^[^\s<>@]+@[^\s<>@]+$/.test(value)
const headerKey = (value: string) => value.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase().replace(/[^a-z0-9]/g, '')

export function decodeContactFile(bytes: Uint8Array) {
  if (bytes.length > maxContactFileBytes) throw new Error('Choose a contact file smaller than 5 MiB.')
  if (bytes[0] === 0xff && bytes[1] === 0xfe) return new TextDecoder('utf-16le').decode(bytes)
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return new TextDecoder('utf-16be').decode(bytes)
  try { return new TextDecoder('utf-8', { fatal: true }).decode(bytes) }
  catch { return new TextDecoder('windows-1252').decode(bytes) }
}

export function csvRows(text: string) {
  const first = text.match(/^(?:[^"\r\n]|"(?:[^"]|"")*")*/)?.[0] || ''
  const counts = [',', ';', '\t'].map(separator => ({ separator, count: first.split(separator).length }))
  const separator = counts.sort((a, b) => b.count - a.count)[0]!.separator
  const rows: string[][] = []; let row: string[] = [], value = '', quoted = false, closed = false
  const field = () => { row.push(value); value = ''; closed = false }
  const record = () => { field(); if (row.some(value => value.trim())) rows.push(row); row = []; if (rows.length > 10001) throw new Error('Import at most 10,000 contact rows at a time.') }
  for (let i = 0; i < text.length; i++) {
    const char = text[i]!
    if (quoted) {
      if (char === '"') { if (text[i + 1] === '"') { value += '"'; i++ } else { quoted = false; closed = true } }
      else value += char
    } else if (char === '"' && !value && !closed) quoted = true
    else if (char === separator) field()
    else if (char === '\r' || char === '\n') { if (char === '\r' && text[i + 1] === '\n') i++; record() }
    else if (closed && !/\s/.test(char)) throw new Error('Invalid quoted CSV field. Export the address book again as CSV.')
    else if (!closed) value += char
  }
  if (quoted) throw new Error('The CSV file contains an unfinished quoted field.')
  if (value || row.length || closed) record()
  return rows
}

function parseCSV(text: string): { entries: ContactImport[]; skipped: number } {
  const rows = csvRows(text), headers = (rows.shift() || []).map(headerKey)
  const indexes = (keys: string[]) => headers.map((name, index) => keys.includes(name) ? index : -1).filter(index => index >= 0)
  const full = indexes(['name', 'fullname', 'displayname', 'nomcomplet', 'nomaffiche', 'nomaffichage', 'formattedname'])
  const given = indexes(['firstname', 'givenname', 'prenom']), middle = indexes(['middlename', 'additionalname', 'deuxiemeprenom'])
  const family = indexes(['lastname', 'familyname', 'nom', 'nomdefamille'])
  const emails = headers.map((name, index) => /^(?:(?:primary|secondary|home|work|other)?(?:email|courriel)(?:(?:address|adresse|value|principal|secondaire)|\d)*|(?:adresseelectronique|adressemessagerie|adressedemessagerie|adressecourriel)\d*(?:principale|secondaire)?)$/.test(name) ? index : -1).filter(index => index >= 0)
  const categories = indexes(['categories', 'category', 'labels', 'libelles', 'groupmembership', 'groups', 'groupes'])
  if (!emails.length) throw new Error('No email column found. Use an Outlook, Google or Thunderbird CSV export, or a vCard file.')
  const entries: ContactImport[] = []; let skipped = 0
  for (const row of rows) {
    const get = (indices: number[]) => indices.map(index => row[index]?.trim() || '').find(Boolean) || ''
    const name = get(full) || [get(given), get(middle), get(family)].filter(Boolean).join(' ')
    const groups = categories.flatMap(index => (row[index] || '').split(/\s*:::\s*|;/)).map(value => value.trim()).filter(value => value && !value.startsWith('*'))
    const values = emails.flatMap(index => (row[index] || '').split(/\s*:::\s*/)).filter(value => value.trim())
    if (!values.length) skipped++
    for (const value of values) {
      const email = value.trim().replace(/^mailto:/i, '')
      if (!validEmail(email) || name.length > 128 || groups.some(group => group.length > 128)) { skipped++; continue }
      entries.push({ name: name || email.slice(0, 128), email, groups })
    }
  }
  return { entries, skipped }
}

function splitCard(value: string, separator: string) {
  const fields: string[] = []; let field = ''
  for (let i = 0; i < value.length; i++) {
    const char = value[i]!
    if (char === '\\' && i + 1 < value.length) { const next = value[++i]!; field += next.toLowerCase() === 'n' ? '\n' : next }
    else if (char === separator) { fields.push(field); field = '' }
    else field += char
  }
  fields.push(field); return fields
}
function parseVCard(text: string): { entries: ContactImport[]; skipped: number } {
  const entries: ContactImport[] = []; let skipped = 0, cards = 0
  let card: { name: string; structured: string; emails: string[]; groups: string[] } | null = null
  // vCard 2.1 soft quoted-printable breaks and RFC line folding.
  const lines = text.replace(/=\r?\n/g, '').replace(/\r?\n[ \t]/g, '').split(/\r\n|\n|\r/)
  for (const line of lines) {
    if (/^BEGIN:VCARD$/i.test(line)) {
      if (card) throw new Error('Invalid nested vCard record.')
      if (++cards > 10000) throw new Error('Import at most 10,000 vCards at a time.')
      card = { name: '', structured: '', emails: [], groups: [] }; continue
    }
    if (/^END:VCARD$/i.test(line)) {
      if (!card) throw new Error('Invalid vCard record.')
      const name = card.name || card.structured
      if (!card.emails.length) skipped++
      for (const email of card.emails) {
        if (!validEmail(email) || name.length > 128 || card.groups.some(group => group.length > 128)) { skipped++; continue }
        entries.push({ name: name || email.slice(0, 128), email, groups: card.groups })
      }
      card = null; continue
    }
    if (!card) continue
    const colon = line.indexOf(':'); if (colon < 0) continue
    const descriptor = line.slice(0, colon), key = descriptor.split(';')[0]!.split('.').pop()!.toUpperCase()
    let value = line.slice(colon + 1)
    if (/;ENCODING=QUOTED-PRINTABLE/i.test(descriptor)) {
      const bytes = Uint8Array.from(Array.from(value.matchAll(/=([0-9A-F]{2})|([^=])/gi), match => match[1] ? parseInt(match[1], 16) : match[2]!.charCodeAt(0)))
      const charset = descriptor.match(/;CHARSET=([^;:]+)/i)?.[1] || 'utf-8'
      try { value = new TextDecoder(charset).decode(bytes) } catch { throw new Error('Unsupported vCard character encoding. Export UTF-8 CSV or vCard.') }
    }
    if (key === 'FN') card.name = splitCard(value, '\0')[0]!.trim()
    if (key === 'N') { const parts = splitCard(value, ';'); card.structured = [parts[3], parts[1], parts[2], parts[0], parts[4]].filter(Boolean).join(' ').trim() }
    if (key === 'EMAIL') card.emails.push(splitCard(value, '\0')[0]!.replace(/^mailto:/i, '').trim())
    if (key === 'CATEGORIES') card.groups.push(...splitCard(value, ',').map(name => name.trim()).filter(Boolean))
  }
  if (card || !cards) throw new Error('The vCard file is incomplete or invalid.')
  return { entries, skipped }
}

export function parseContactFile(text: string) {
  text = text.replace(/^\uFEFF/, '')
  return /^\s*BEGIN:VCARD/i.test(text) ? parseVCard(text.trim()) : parseCSV(text)
}

function cardEscape(value: string) { return value.replace(/\\/g, '\\\\').replace(/\r\n|\r|\n/g, '\\n').replace(/;/g, '\\;').replace(/,/g, '\\,') }
function foldCard(value: string) {
  let result = '', count = 0
  for (const char of value) {
    const size = new TextEncoder().encode(char).length
    if (count + size > 75) { result += '\r\n '; count = 1 }
    result += char; count += size
  }
  return result
}
export function exportContactFile(book: ContactBook, format: ContactFormat) {
  const names = new Map(book.groups.map(group => [group.id, group.name]))
  const groups = (ids: string[] = []) => ids.map(id => names.get(id)).filter((name): name is string => !!name)
  if (format === 'vcard') {
    const text = book.contacts.map(contact => ['BEGIN:VCARD', 'VERSION:3.0', `FN:${cardEscape(contact.name)}`, `N:;${cardEscape(contact.name)};;;`, `EMAIL;TYPE=INTERNET:${cardEscape(contact.email)}`, ...(contact.group_ids?.length ? [`CATEGORIES:${groups(contact.group_ids).map(cardEscape).join(',')}`] : []), 'END:VCARD'].map(foldCard).join('\r\n')).join('\r\n') + '\r\n'
    return { text, filename: 'contacts.vcf', type: 'text/vcard;charset=utf-8' }
  }
  const headers = format === 'google' ? ['First Name', 'Last Name', 'Email 1 - Label', 'Email 1 - Value', 'Labels'] : format === 'thunderbird' ? ['First Name', 'Last Name', 'Display Name', 'Primary Email', 'Categories'] : ['First Name', 'Last Name', 'E-mail Address', 'Categories']
  const rows = book.contacts.map(contact => format === 'google' ? [contact.name, '', '', contact.email, groups(contact.group_ids).join(' ::: ')] : format === 'thunderbird' ? [contact.name, '', contact.name, contact.email, groups(contact.group_ids).join(';')] : [contact.name, '', contact.email, groups(contact.group_ids).join(';')])
  // Quote every cell, including embedded line breaks, and use a UTF-8 BOM for Outlook.
  const text = '\uFEFF' + [headers, ...rows].map(row => row.map(value => '"' + value.replace(/"/g, '""') + '"').join(',')).join('\r\n') + '\r\n'
  return { text, filename: `contacts-${format}.csv`, type: 'text/csv;charset=utf-8' }
}
