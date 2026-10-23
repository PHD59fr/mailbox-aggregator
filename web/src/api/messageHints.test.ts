import {expect,it} from 'vitest'
import {messageHints} from './messageHints'
import type {Message} from './reading'
const mail=(from:Message['from'],reply_to:Message['reply_to']=[])=>({from,reply_to} as Message)
it('does not treat image permissions or a same-domain reply address as authentication evidence',()=>{
 expect(messageHints(mail([{name:'Support (support@example.test)',email:'support@example.test'}],[{email:'replies@example.test'}]))).toEqual([])
})
it('describes a different reply domain and displayed email without claiming fraud',()=>{
 const hints=messageHints(mail([{name:'finance@example.test',email:'sender@other.test'}],[{email:'replies@different.test'}]))
 expect(hints).toHaveLength(2);expect(hints.join(' ')).toContain('Verify');expect(hints.join(' ')).not.toMatch(/guaranteed|authenticated|fraud detected/i)
})
it('makes an internationalized sender domain visible for manual verification',()=>{
 expect(messageHints(mail([{email:'sender@xn--example-123.test'}]))[0]).toContain('internationalized')
})
