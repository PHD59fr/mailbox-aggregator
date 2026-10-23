import { describe, expect, it } from 'vitest'
import { encodeAttachments, validateAttachments, MAX_ATTACHMENT_BYTES } from './attachments'

describe('memory-only attachments', () => {
  it('encodes binary content and filenames without modifying the bytes', async () => {
    const file = new File([new Uint8Array([0, 1, 255, 128])], 'rapport été.bin')
    expect(await encodeAttachments([file])).toEqual([{ filename: 'rapport été.bin', content_type: 'application/octet-stream', data: 'AAH/gA==' }])
  })
  it('allows the size boundary and rejects aggregate size and file-count overflow', () => {
    const file = new File([new Uint8Array(MAX_ATTACHMENT_BYTES)], 'full.bin')
    expect(() => validateAttachments([file])).not.toThrow()
    expect(() => validateAttachments([file, new File(['x'], 'overflow.txt')])).toThrow('10 MB')
    expect(() => validateAttachments(Array.from({ length: 21 }, () => new File([], 'empty.txt')))).toThrow('20 files')
  })
})
