import { describe, expect, it } from 'vitest'
import { actionDraft, recipientReplySender } from './mailActions'
import type { Message } from './reading'
import type { SMTPConfig } from './accounts'
const message: Message = { uid: 12, uid_validity: 7, subject: 'Project', from: [{ name: '<Team>', email: 'from@example.test' }], reply_to: [{ email: 'reply@example.test' }], to: [{ email: 'me@example.test' }, { email: 'other@example.test' }], cc: [{ email: 'other@example.test' }, { email: 'copy@example.test' }], date: '2026-10-22T12:00:00Z', seen: false, size: 100, text: 'Hello', html: '<p>Hello</p>', attachments: [], truncated: false, message_id: 'original@example.test' }
const sender = (id:string,email:string,is_default=false):SMTPConfig => ({id,name:id,host:'smtp.test',port:465,security:'tls',username:'login-not-sender',sender_email:email,is_default})
describe('reply sender selection',()=>{
  it('matches To before Cc, ignoring case and surrounding whitespace',()=>{
    const first=sender('first','fallback@example.test',true), cc=sender('cc','copy@example.test'), to=sender('to',' ME@example.test ')
    expect(recipientReplySender([first,cc,to],message)?.id).toBe('to')
    expect(recipientReplySender([first,cc],message)?.id).toBe('cc')
  })
  it('falls back to the first available sender, not a linked/default identity',()=>{
    const first=sender('first','first@example.test'), preferred=sender('default','default@example.test',true)
    expect(recipientReplySender([first,preferred],message)?.id).toBe('first')
    expect(recipientReplySender([sender('incomplete',''),first,preferred],message)?.id).toBe('first')
    expect(recipientReplySender([first],{to:[],cc:[]})?.id).toBe('first')
    expect(recipientReplySender([],message)).toBeUndefined()
  })
  it('never matches From, Reply-To, SMTP login, domains or stripped plus aliases',()=>{
    const first=sender('first','fallback@example.test'), from=sender('from','from@example.test'), replyTo=sender('reply-to','reply@example.test'), base=sender('base','base@example.test')
    base.username='alias+one@example.test'
    const received={...message,to:[{email:'alias+one@example.test'},{email:'base+tag@example.test'}],cc:[]}
    expect(recipientReplySender([first,from,replyTo,base],received)?.id).toBe('first')
    expect(recipientReplySender([first,sender('alias','base+tag@example.test'),base],received)?.id).toBe('alias')
  })
})
describe('mail action drafts', () => {
  it('replies to Reply-To and keeps threading without sending anything', () => {
    const draft = actionDraft('reply',message,['me@example.test'])
    expect(draft.to).toBe('reply@example.test'); expect(draft.cc).toBe(''); expect(draft.subject).toBe('Re: Project')
    expect(draft.in_reply_to).toBe('<original@example.test>'); expect(draft.html).toContain('&lt;Team&gt;')
  })
  it('reply-all excludes own identities and deduplicates recipients', () => {
    const draft = actionDraft('reply-all',message,['me@example.test'])
    expect(draft.to).toBe('reply@example.test'); expect(draft.cc).toBe('other@example.test, copy@example.test')
  })
  it('forwards without retaining original recipients or threading', () => {
    const draft = actionDraft('forward',message,[])
    expect(draft.to).toBe(''); expect(draft.cc).toBe(''); expect(draft.subject).toBe('Fwd: Project'); expect(draft.in_reply_to).toBeUndefined()
  })
  it('redirect keeps original subject and does not create an editable replacement body', () => {
    const draft = actionDraft('redirect',message,[])
    expect(draft.subject).toBe('Project'); expect(draft.html).toBe(''); expect(draft.to).toBe('')
  })
})
