/* eslint-disable no-control-regex -- Reject control characters in untrusted Message-IDs. */
// Keep the root and newest parents, with the same limits as the Go API.
export function referencesForReply(references: string[] = [], parent?: string, inReplyTo?: string): string[] {
  const chain = references.length ? references : inReplyTo?.match(/<[^<>\s\x00-\x1f\x7f]+>/g) || []
  const ids = [...chain, ...(parent ? [parent.startsWith('<') ? parent : `<${parent}>`] : [])]
  const unique = [...new Set(ids.filter(id => id.length <= 1000 && /^<[^<>\s\x00-\x1f\x7f]+>$/.test(id)))]
  return unique.length > 32 ? [unique[0]!, ...unique.slice(-31)] : unique
}
