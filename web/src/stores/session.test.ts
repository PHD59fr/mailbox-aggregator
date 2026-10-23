import {createPinia,setActivePinia} from 'pinia'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {useSessionStore} from './session'
beforeEach(()=>{vi.useFakeTimers();setActivePinia(createPinia())})
afterEach(()=>{useSessionStore().dismissNotice();vi.useRealTimers()})
describe('temporary workspace notifications',()=>{
  it('automatically removes a new-mail notification after six seconds',async()=>{
    const session=useSessionStore();session.showNotice('New mail in Work.')
    await vi.advanceTimersByTimeAsync(5999);expect(session.notice).toBe('New mail in Work.')
    await vi.advanceTimersByTimeAsync(1);expect(session.notice).toBe('')
  })
  it('restarts the timeout even for another message in the same mailbox',async()=>{
    const session=useSessionStore();session.showNotice('New mail in Work.')
    await vi.advanceTimersByTimeAsync(5000);session.showNotice('New mail in Work.')
    await vi.advanceTimersByTimeAsync(1000);expect(session.notice).toBe('New mail in Work.')
    await vi.advanceTimersByTimeAsync(5000);expect(session.notice).toBe('')
  })
  it('does not let an old timeout clear a newer notification and cancels on lock/dismissal',async()=>{
    const session=useSessionStore();session.showNotice('New mail in Work.')
    await vi.advanceTimersByTimeAsync(5000);session.showNotice('Message sent.')
    await vi.advanceTimersByTimeAsync(1000);expect(session.notice).toBe('Message sent.')
    session.dismissNotice();expect(vi.getTimerCount()).toBe(0)
    session.showNotice('Private mailbox');session.lockLocal();expect(session.notice).toBe('');expect(vi.getTimerCount()).toBe(0)
  })
  it('keeps a delivery outcome until the vault is unlocked again',()=>{
    const session=useSessionStore()
    session.$patch({authenticated:true,userID:'test-user',vmk:null})
    session.deferNotice('Message sent.')
    expect(session.notice).toBe('')
    expect(session.deferredNotice).toBe('Message sent.')
    expect(session.deferredNoticeKind).toBe('success')

    session.$patch({vmk:new Uint8Array(32).fill(7)})
    session.flushDeferredNotice()
    expect(session.notice).toBe('Message sent.')
    expect(session.deferredNotice).toBe('')
  })
  it('shows a delivery outcome directly while the vault is still unlocked',()=>{
    const session=useSessionStore()
    session.$patch({authenticated:true,userID:'test-user',vmk:new Uint8Array(32).fill(7)})
    session.deferNotice('Delivery was not confirmed.','warning')
    expect(session.notice).toBe('Delivery was not confirmed.')
    expect(session.noticeKind).toBe('warning')
    expect(session.deferredNotice).toBe('')
  })
  it('drops deferred delivery state when the local session ends',()=>{
    const session=useSessionStore()
    session.$patch({vmk:new Uint8Array(32).fill(7),deferredNotice:'Message sent.',sentDraftID:'draft-id'})
    session.clearLocalSession()
    expect(session.deferredNotice).toBe('')
    expect(session.sentDraftID).toBe('')
  })
})
