import {mount,flushPromises} from '@vue/test-utils'
import {createPinia,setActivePinia} from 'pinia'
import {createMemoryHistory,createRouter} from 'vue-router'
import {beforeEach,describe,expect,it} from 'vitest'
import AppHeader from './AppHeader.vue'
import {useSessionStore} from '../stores/session'
import {useMailboxStore} from '../stores/mailboxes'
beforeEach(()=>{setActivePinia(createPinia());useSessionStore().$patch({authenticated:true,loading:false,userID:'user',vmk:new Uint8Array(32)})})
async function header(){
  const router=createRouter({history:createMemoryHistory(),routes:['/settings','/mail','/compose','/contacts'].map(path=>({path,component:{template:'<div />'}}))})
  await router.push('/settings');await router.isReady()
  return {router,wrapper:mount(AppHeader,{global:{plugins:[router]}})}
}
describe('mail search header',()=>{
  it('opens the dedicated Contacts workspace from the main navigation',async()=>{
    const {router,wrapper}=await header()
    await wrapper.get('.rail-link[href="/contacts"]').trigger('click');await flushPromises()
    expect(router.currentRoute.value.path).toBe('/contacts')
    expect(wrapper.get('.breadcrumb strong').text()).toBe('Contacts')
    wrapper.unmount()
  })
  it('shares a search with the inbox and navigates there on Enter',async()=>{
    const {router,wrapper}=await header()
    await wrapper.get('input[type="search"]').setValue('old project')
    expect(useMailboxStore().search).toBe('old project')
    await wrapper.get('[role="search"]').trigger('submit');await flushPromises()
    expect(router.currentRoute.value.path).toBe('/mail');wrapper.unmount()
  })
  it('opens mail filters from any workspace page and disables search when locked',async()=>{
    const {router,wrapper}=await header()
    await wrapper.get('[aria-label="Filter mail"]').trigger('click');await flushPromises()
    expect(useMailboxStore().filtersOpen).toBe(true);expect(router.currentRoute.value.path).toBe('/mail')
    useSessionStore().lockLocal();await flushPromises()
    expect(wrapper.get('input[type="search"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[aria-label="Filter mail"]').attributes('disabled')).toBeDefined();wrapper.unmount()
  })
})
