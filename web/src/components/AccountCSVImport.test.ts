import {beforeEach,expect,it,vi} from 'vitest'
import {mount,flushPromises} from '@vue/test-utils'
import {createMemoryHistory,createRouter} from 'vue-router'
import {createPinia,setActivePinia} from 'pinia'
import AccountCSVImport from './AccountCSVImport.vue'
import {useSessionStore} from '../stores/session'
import {listAccounts,listSMTPConfigs} from '../api/accounts'
import {importAccountCSV} from '../composables/useImportExport'
vi.mock('../api/accounts',()=>({listAccounts:vi.fn(),listSMTPConfigs:vi.fn()}))
vi.mock('../composables/useImportExport',()=>({importAccountCSV:vi.fn()}))
beforeEach(()=>{vi.resetAllMocks();setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,userID:'user',vmk:new Uint8Array(32)});vi.mocked(listAccounts).mockResolvedValue({});vi.mocked(listSMTPConfigs).mockResolvedValue({})})
async function preview(){const router=createRouter({history:createMemoryHistory(),routes:[{path:'/settings',component:{template:'<div />'}},{path:'/login',component:{template:'<div />'}}]});await router.push('/settings');const wrapper=mount(AccountCSVImport,{global:{plugins:[router]}})
 const file=new File(['name,imap_host,imap_port,imap_security,imap_username,imap_password\nWork,imap.example.test,993,tls,alice,private-provider-password'],'accounts.csv',{type:'text/csv'})
 const input=wrapper.get('input[type="file"]');Object.defineProperty(input.element,'files',{value:[file],configurable:true});await input.trigger('change');await flushPromises();await vi.waitFor(()=>expect(wrapper.text()).toContain('Work'));return{wrapper,router}}
it('previews without exposing passwords, requires confirmation and navigates to sign-in after import',async()=>{
 const {wrapper,router}=await preview();expect(wrapper.text()).toContain('Work');expect(wrapper.html()).not.toContain('private-provider-password');expect(importAccountCSV).not.toHaveBeenCalled()
 const save=wrapper.findAll('button').find(b=>b.text()==='Encrypt and import accounts')!;expect(save.attributes('disabled')).toBeDefined();await wrapper.get('input[type="checkbox"]').setValue(true)
 vi.mocked(importAccountCSV).mockImplementation(async()=>{useSessionStore().clearLocalSession();return{accounts:{},smtp:{},added:1,duplicates:0}})
 await save.trigger('click');await flushPromises();expect(router.currentRoute.value.path).toBe('/login');wrapper.unmount()
})
it('keeps the preview and unlocked session when the atomic import conflicts',async()=>{
 const {wrapper}=await preview();await wrapper.get('input[type="checkbox"]').setValue(true);vi.mocked(importAccountCSV).mockRejectedValue(new Error('Configuration changed. Reload first.'))
 await wrapper.findAll('button').find(b=>b.text()==='Encrypt and import accounts')!.trigger('click');await flushPromises()
 expect(wrapper.get('[role="alert"]').text()).toContain('Configuration changed');expect(useSessionStore().unlocked).toBe(true);expect(wrapper.text()).toContain('Work');wrapper.unmount()
})
