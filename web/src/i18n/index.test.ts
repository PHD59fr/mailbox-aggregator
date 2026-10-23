import {createRouter,createMemoryHistory} from 'vue-router'
import {createPinia,setActivePinia} from 'pinia'
import {flushPromises,mount} from '@vue/test-utils'
import {afterEach,beforeEach,describe,expect,it,vi} from 'vitest'
import {direction,formatDate,formatNumber,initializeLanguage,locale,plural,setLanguage,t} from './index'
import LanguageSettings from '../components/LanguageSettings.vue'
import AppHeader from '../components/AppHeader.vue'
import {useSessionStore} from '../stores/session'
import {APIError} from '../api/client'
import {useLanguagePreferences} from './preferences'
import {readPreference,updatePreference} from '../api/preferences'
import MessageReader from '../components/MessageReader.vue'
import {generateSieve,newSieveRule} from '../api/sieveRules'
import {actionDraft} from '../api/mailActions'
import type {Message} from '../api/reading'
import en from './en.json'
import fr from './fr.json'

vi.mock('../api/preferences',()=>({readPreference:vi.fn(),updatePreference:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());localStorage.clear();setLanguage('en','auto',false)})
afterEach(()=>{setLanguage('en','auto',false);localStorage.clear()})
describe('interface language and direction',()=>{
  it('keeps catalog keys and placeholder contracts in sync',()=>{
    expect(Object.keys(fr).sort()).toEqual(Object.keys(en).sort())
    const placeholders=(s:string)=>(s.match(/\{\w+\}/g)||[]).sort()
    for(const [key,value] of Object.entries(fr)) { expect(value.trim(),key).not.toBe('');expect(placeholders(value),key).toEqual(placeholders(key)) }
  })
  it('uses explicit choice, French formats and plurals, and keeps parameters untouched',()=>{
    expect(locale.value).toBe('en');setLanguage('fr','rtl')
    expect(document.documentElement.lang).toBe('fr');expect(document.documentElement.dir).toBe('rtl')
    expect(t('Add selected Google recipients')).toBe('Ajouter les destinataires Google sélectionnés')
    expect(plural('{count} mailbox','{count} mailboxes',1)).toBe('1 boîte')
    expect(plural('{count} mailbox','{count} mailboxes',2)).toBe('2 boîtes')
    expect(formatNumber(12345)).toBe(new Intl.NumberFormat('fr').format(12345))
    expect(formatDate('2026-10-26T12:00:00Z',{year:'numeric',month:'long',day:'numeric'})).toContain('octobre')
    expect(t('Select {0}',{'0':'Settings <alice@example.test>'})).toBe('Sélectionner Settings <alice@example.test>')
    expect(t('constructor')).toBe('constructor')
    expect(new APIError('MFA_FAILED','Authentication failed.',401)).toMatchObject({code:'MFA_FAILED',message:'Échec de l’authentification.',status:401})
    setLanguage('en','auto',false);initializeLanguage()
    expect(locale.value).toBe('fr');expect(direction.value).toBe('rtl')
  })
  it('changes navigation labels from encrypted profile settings',async()=>{
    const router=createRouter({history:createMemoryHistory(),routes:[{path:'/mail',component:{template:'<div />'}}]});await router.push('/mail')
    const header=mount(AppHeader,{global:{plugins:[router],stubs:{RouterLink:{template:'<a><slot /></a>'}}}})
    useSessionStore().$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})
    useLanguagePreferences().loaded=true
    vi.mocked(updatePreference).mockImplementation(async()=>({id:'interface-language'}))
    const settings=mount(LanguageSettings)
    await settings.findAll('select')[0]!.setValue('fr');await flushPromises()
    expect(header.text()).toContain('Boîte de réception')
    await settings.findAll('select')[1]!.setValue('rtl');await flushPromises()
    expect(document.documentElement.dir).toBe('rtl')
    settings.unmount();header.unmount()
  })
  it('ignores old profile loads and saves after lock',async()=>{
    const session=useSessionStore();session.$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})
    let resolve!:(value:{id:'interface-language';locale:'fr';direction:'rtl'})=>void
    vi.mocked(readPreference).mockReturnValue(new Promise(r=>{resolve=r}))
    const preferences=useLanguagePreferences(),pending=preferences.load()
    session.lockLocal();preferences.clear();resolve({id:'interface-language',locale:'fr',direction:'rtl'})
    await pending;expect(locale.value).toBe('en');expect(preferences.loaded).toBe(false)
    await preferences.save('fr','rtl');expect(updatePreference).not.toHaveBeenCalled()
  })
  it('keeps message content, reply subjects and generated provider commands unchanged',async()=>{
    const message:Message={uid:1,uid_validity:7,subject:'Inbox',from:[{name:'Settings',email:'Contacts@example.test'}],to:[],cc:[],date:'2026-10-26',seen:true,size:100,text:'Settings',html:'',attachments:[],truncated:false,message_id:'parent@example.test'}
    const rule={...newSieveRule(),action:'fileinto' as const,destination:'Sent',conditions:[{field:'subject' as const,match:'is' as const,value:'Inbox'}]}
    const script=generateSieve([rule],['fileinto'])
    setLanguage('fr','rtl',false)
    const wrapper=mount(MessageReader,{props:{message,loading:false,error:'',downloading:''}});await flushPromises()
    expect(wrapper.get('.message-plaintext').text()).toBe('Settings')
    expect(wrapper.text()).toContain('Inbox');expect(wrapper.text()).toContain('Répondre')
    expect(generateSieve([rule],['fileinto'])).toBe(script)
    expect(actionDraft('reply',message,[])).toMatchObject({subject:'Re: Inbox',in_reply_to:'<parent@example.test>'})
    wrapper.unmount()
  })
  it('saves a profile choice through encrypted preferences before applying it',async()=>{
    const session=useSessionStore();session.$patch({authenticated:true,userID:'u',vmk:new Uint8Array(32)})
    vi.mocked(readPreference).mockImplementation(async()=>({id:'interface-language',locale:'en',direction:'auto'}))
    vi.mocked(updatePreference).mockImplementation(async()=>({id:'interface-language',locale:'fr',direction:'rtl'}))
    const preferences=useLanguagePreferences();await preferences.load();await preferences.save('fr','rtl');await flushPromises()
    expect(updatePreference).toHaveBeenCalledWith('interface-language',expect.any(Function),expect.any(Function))
    expect(locale.value).toBe('fr');expect(direction.value).toBe('rtl')
  })
})
