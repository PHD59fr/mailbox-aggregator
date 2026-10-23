export const MAX_ATTACHMENT_BYTES = 10 * 1024 * 1024
export const MAX_ATTACHMENTS = 20
export type AttachmentFile = File
export type AttachmentInput = { files: FileList | null; value: string }

export function validateAttachments(files: readonly File[]): void {
  if (files.length > MAX_ATTACHMENTS) throw new Error('You can attach up to 20 files.')
  if (files.reduce((total, file) => total + file.size, 0) > MAX_ATTACHMENT_BYTES) {
    throw new Error('Attachments must total 10 MB or less.')
  }
}

export async function encodeAttachments(files: readonly File[]) {
  validateAttachments(files)
  return Promise.all(files.map(file => new Promise<{ filename: string; content_type: string; data: string }>((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error(`Could not read ${file.name}. Please attach it again.`))
    reader.onabort = () => reject(new Error('Reading the attachment was cancelled.'))
    reader.onload = () => {
      if (typeof reader.result !== 'string' || !reader.result.includes(',')) {
        reject(new Error('Could not read the attachment.'))
        return
      }
      resolve({ filename: file.name, content_type: file.type || 'application/octet-stream', data: reader.result.slice(reader.result.indexOf(',') + 1) })
    }
    reader.readAsDataURL(file)
  })))
}
