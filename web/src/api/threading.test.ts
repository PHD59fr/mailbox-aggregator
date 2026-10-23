import {describe, expect, it} from 'vitest'
import {actionDraft} from './mailActions'
import {referencesForReply} from './threading'
import type {Message} from './reading'

const message: Message = {uid:1, uid_validity:7, subject:'Thread', from:[{email:'writer@example.test'}], to:[], cc:[], date:'2026-10-26', seen:true, size:100, text:'body', html:'', attachments:[], truncated:false, message_id:'parent@example.test', in_reply_to:'<root@example.test>', references:['<root@example.test>', '<older@example.test>']}
describe('reply threading', () => {
  it('retains root and newest ancestors within API bounds, filtering injection and duplicates', () => {
    const input = Array.from({length:40}, (_,n)=>`<${n}@example.test>`)
    const chain = referencesForReply([...input, input[39]!, '<bad\r\nBcc:x>'], 'parent@example.test')
    expect(chain).toHaveLength(32)
    expect(chain[0]).toBe(input[0]); expect(chain[chain.length-1]).toBe('<parent@example.test>')
    expect(chain.some(id=>id.includes('Bcc'))).toBe(false)
  })
  it.each(['reply','reply-all'] as const)('appends parent to %s, with In-Reply-To fallback for legacy parents', action => {
    expect(actionDraft(action, message, []).references).toEqual([...message.references!, '<parent@example.test>'])
    expect(actionDraft(action, {...message,references:undefined}, []).references).toEqual(['<root@example.test>', '<parent@example.test>'])
  })
  it('preserves draft references and leaves forwards unrelated', () => {
    expect(actionDraft('draft',message,[]).references).toEqual(message.references)
    expect(actionDraft('forward',message,[]).references).toBeUndefined()
  })
})
