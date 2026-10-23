import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {createRouter,createMemoryHistory} from 'vue-router'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import MailView from './MailView.vue'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
import {listFolders,listMessages,readMessage,type Message,type MessagePage} from '../api/reading'
import {useMailActions} from '../stores/mailActions'
import MessageReader from '../components/MessageReader.vue'
vi.mock('../api/reading',()=>({listFolders:vi.fn(),listMessages:vi.fn(),readMessage:vi.fn(),downloadAttachment:vi.fn(),markMessageSeen:vi.fn()}))
beforeEach(()=>{
  vi.resetAllMocks();setActivePinia(createPinia())
  useSessionStore().$patch({authenticated:true,loading:false,userID:'user',vmk:new Uint8Array(32)})
  vi.mocked(listMessages).mockResolvedValue({messages:[],uid_validity:7,next_before:0,total:0})
})
describe('mailbox navigation',()=>{
  it('announces a pending search in the loading banner and message heading',async()=>{
    vi.useFakeTimers()
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true}]
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    vi.mocked(listMessages).mockImplementationOnce(()=>new Promise(()=>{}))
    useMailboxStore().search='invoice';await flushPromises()
    expect(wrapper.get('.mail-loading').text()).toContain('Searching messages…')
    expect(wrapper.get('.message-list-loading').text()).toBe('Searching messages…')
    expect(wrapper.get('.pane-title h2').text()).toBe('Searching…')
    await vi.advanceTimersByTimeAsync(400);await flushPromises()
    expect(wrapper.get('.mail-loading').text()).toContain('Applying your search and filters.')
    wrapper.unmount();vi.useRealTimers()
  })
  it('shows every message load above a long folder list and keeps existing messages visible during refresh',async()=>{
    const message:Message={uid:1,uid_validity:7,subject:'Mail',from:[],to:[],cc:[],date:'2026-10-22T12:00:00Z',seen:true,size:100,text:'Hello',html:'',attachments:[],truncated:false}
    let finish!:(page:MessagePage)=>void
    vi.mocked(listMessages).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
    vi.mocked(listFolders).mockResolvedValue({folders:Array.from({length:80},(_,index)=>({name:`Folder ${index}`,delimiter:'/',selectable:true,messages:0,unseen:0,uid_next:1,uid_validity:7})),truncated:false})
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true}]
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    expect(wrapper.findAll('.folder-select')).toHaveLength(80)
    expect(wrapper.get('.mail-loading[role="status"]').text()).toContain('Fetching the latest messages.')
    expect(wrapper.get('.mail-loading').element.compareDocumentPosition(wrapper.get('.accounts').element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(wrapper.get('.message-list').attributes('aria-busy')).toBe('true')
    expect(wrapper.get('.message-list-loading').text()).toBe('Loading messages…')
    expect(wrapper.find('.message-list .empty-mail').exists()).toBe(false)
    finish({messages:[message],uid_validity:7,next_before:0,total:1});await flushPromises()
    expect(wrapper.find('.mail-loading').exists()).toBe(false)
    expect(wrapper.find('.message-list-loading').exists()).toBe(false)
    await wrapper.get('.folder-select[title="Folder 10"]').trigger('click');await flushPromises()
    expect(wrapper.get('.message-list-loading').text()).toBe('Loading messages…')
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'Folder 10')
    finish({messages:[message],uid_validity:7,next_before:0,total:1});await flushPromises()
    expect(wrapper.find('.message-list-loading').exists()).toBe(false)
    await wrapper.get('[aria-label="Refresh"]').trigger('click');await flushPromises()
    expect(wrapper.find('.mail-loading').exists()).toBe(true)
    expect(wrapper.get('.message-list-loading').text()).toBe('Loading messages…')
    expect(wrapper.findAll('.message-row')).toHaveLength(1)
    finish({messages:[message],uid_validity:7,next_before:0,total:1});await flushPromises()
    expect(wrapper.find('.mail-loading').exists()).toBe(false)
    expect(wrapper.get('.message-list').attributes('aria-busy')).toBe('false')
    wrapper.unmount()
  })
  it('disables Delete for the selected account in a merged inbox when Trash is None',async()=>{
    const message:Message={uid:1,uid_validity:7,subject:'Mail',from:[],to:[],cc:[],date:'2026-10-22T12:00:00Z',seen:true,size:100,text:'Hello',html:'',attachments:[],truncated:false}
    vi.mocked(listMessages).mockResolvedValue({messages:[message],uid_validity:7,next_before:0,total:1});vi.mocked(readMessage).mockResolvedValue(message)
    useMailboxStore().accounts=[
      {id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,folder_mapping:{trash:null}},
      {id:'other',name:'Other',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'other',is_default:false},
    ]
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView},{path:'/settings',component:{template:'<div />'}}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    const workRow=wrapper.findAll('.message-row').find(row=>row.get('.message-account').text()==='Work')!
    await workRow.trigger('click');await flushPromises()
    expect(wrapper.findComponent(MessageReader).props('trashDisabled')).toBe(true)
    wrapper.unmount()
  })
  it('uses the manual folder role for navigation, heading and the reader Trash guard',async()=>{
    const message:Message={uid:1,uid_validity:7,subject:'Mail',from:[],to:[],cc:[],date:'2026-10-22T12:00:00Z',seen:true,size:100,text:'Hello',html:'',attachments:[],truncated:false}
    vi.mocked(listMessages).mockResolvedValue({messages:[message],uid_validity:7,next_before:0,total:1});vi.mocked(readMessage).mockResolvedValue(message)
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Personal/Deleted',delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:2,uid_validity:7}],truncated:false})
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true,folder_mapping:{trash:'Personal/Deleted'}}]
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView},{path:'/settings',component:{template:'<div />'}}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    expect(wrapper.get('.synced-folders .folder-select').text()).toBe('Trash')
    await wrapper.get('.synced-folders .folder-select').trigger('click');await flushPromises()
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'Personal/Deleted')
    expect(wrapper.get('.workspace-heading h1').text()).toBe('Trash')
    await wrapper.get('.message-row').trigger('click');await flushPromises()
    expect(wrapper.findComponent(MessageReader).props('inTrash')).toBe(true)
    wrapper.unmount()
  })
  it('shows a nested folder name and readable breadcrumb while preserving its server path',async()=>{
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Loyer',delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:2,uid_validity:7},{name:'Loyer/adhoc',delimiter:'/',selectable:true,messages:1,unseen:0,uid_next:2,uid_validity:7}],truncated:false})
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true}]
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    await wrapper.get('[aria-label="Expand Loyer"]').trigger('click')
    await wrapper.get('.folder-select[title="Loyer/adhoc"]').trigger('click');await flushPromises()
    expect(wrapper.get('.workspace-heading h1').text()).toBe('adhoc')
    expect(wrapper.get('.pane-title .eyebrow').text()).toBe('Work / Loyer / adhoc')
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'Loyer/adhoc')
    wrapper.unmount()
  })
  it.each([true,false])('carries receiving-account reply preference %s into the composer',async enabled=>{
    const message:Message={uid:1,uid_validity:7,subject:'Mail',from:[{email:'team@example.test'}],to:[{email:'alias@example.test'}],cc:[],date:'2026-10-22T12:00:00Z',seen:true,size:100,text:'Hello',html:'',attachments:[],truncated:false}
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,smtp_config_id:'linked',reply_from_recipient:enabled}]
    vi.mocked(listMessages).mockResolvedValue({messages:[message],uid_validity:7,next_before:0,total:1});vi.mocked(readMessage).mockResolvedValue(message)
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView},{path:'/compose',component:{template:'<div />'}}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true,MessageReader:true}}});await flushPromises()
    await wrapper.get('.message-row').trigger('click');await flushPromises()
    wrapper.findComponent(MessageReader).vm.$emit('action','reply');await flushPromises()
    expect(useMailActions().pending).toMatchObject({action:'reply',replyFromRecipient:enabled,smtpConfigID:'linked'})
    expect(router.currentRoute.value.path).toBe('/compose');wrapper.unmount()
  })
  it('exposes combined read/sender filters and clears the current search with them',async()=>{
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView},{path:'/settings',component:{template:'<div />'}}]})
    await router.push('/');await router.isReady()
    useMailboxStore().filtersOpen=true
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true}}});await flushPromises()
    await wrapper.get('.mail-filters select').setValue('unread');await wrapper.get('.mail-filters input').setValue('team@example.test')
    expect(useMailboxStore().readFilter).toBe('unread');expect(useMailboxStore().senderFilter).toBe('team@example.test')
    useMailboxStore().search='project';await flushPromises()
    await wrapper.get('.mail-filters button').trigger('click');expect(useMailboxStore().filtering).toBe(false);wrapper.unmount()
  })
  it('shows Add mailbox only before the first account is added, and displays synchronized folders',async()=>{
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:MailView},{path:'/settings',component:{template:'<div />'}}]})
    await router.push('/');await router.isReady()
    const wrapper=mount(MailView,{global:{plugins:[router],stubs:{AppHeader:true}}});await flushPromises()
    expect(wrapper.find('.add-link').exists()).toBe(true)
    vi.mocked(listFolders).mockResolvedValue({folders:[{name:'Sent',delimiter:'/',selectable:true,messages:1,unseen:1,uid_next:2,uid_validity:7}],truncated:false})
    useMailboxStore().accounts=[{id:'work',name:'Work',imap_host:'imap.test',imap_port:993,imap_security:'tls',imap_username:'me',is_default:true,sync_folders:true}]
    await flushPromises()
    expect(wrapper.find('.add-link').exists()).toBe(false)
    expect(wrapper.find('.synced-folders').text()).toContain('Sent')
    await wrapper.find('.synced-folders button').trigger('click');await flushPromises()
    expect(listMessages).toHaveBeenLastCalledWith('work',expect.any(AbortSignal),0,0,'Sent')
    wrapper.unmount()
  })
})
