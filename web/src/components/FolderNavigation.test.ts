import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FolderNavigation from './FolderNavigation.vue'
import type { MailFolder } from '../api/reading'

const folder=(name:string,selectable=true):MailFolder=>({name,selectable,delimiter:'/',messages:1,unseen:1,uid_next:2,uid_validity:7})
const props={folders:[folder('Loyer'),folder('Loyer/adhoc'),folder('Loyer/nexim')],accountId:'work',accountName:'Work',active:''}
describe('folder navigation',()=>{
  it('collapses custom subfolders, separates opening the parent from expanding, and emits exact child paths',async()=>{
    const wrapper=mount(FolderNavigation,{props})
    expect(wrapper.find('.folder-children').exists()).toBe(false)
    await wrapper.get('.folder-select').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['Loyer']])
    await wrapper.get('[aria-label="Expand Loyer"]').trigger('click')
    expect(wrapper.get('.folder-children').findAll('.folder-select span').map(span=>span.text())).toEqual(['adhoc','nexim'])
    await wrapper.get('.folder-children .folder-select[title="Loyer/nexim"]').trigger('click')
    expect(wrapper.emitted('select')?.[1]).toEqual(['Loyer/nexim'])
    await wrapper.get('[aria-label="Collapse Loyer"]').trigger('click')
    await wrapper.setProps({folders:props.folders.map(item=>({...item,unseen:2}))})
    expect(wrapper.find('.folder-children').exists()).toBe(false)
    wrapper.unmount()
  })
  it('reveals an active nested folder and never opens a synthetic parent',async()=>{
    const wrapper=mount(FolderNavigation,{props:{...props,folders:[folder('Loyer/adhoc')],active:'Loyer/adhoc'}})
    expect(wrapper.get('.folder-container').text()).toBe('Loyer')
    expect(wrapper.get('.folder-select.active').attributes('title')).toBe('Loyer/adhoc')
    expect(wrapper.findAll('.folder-select')).toHaveLength(1)
    expect(wrapper.find('[aria-label="Actions for Loyer"]').exists()).toBe(false)
    expect(wrapper.find('summary[aria-label="Actions for Loyer/adhoc"]').exists()).toBe(true)
    await wrapper.get('[aria-label="Collapse Loyer"]').trigger('click')
    expect(wrapper.find('.folder-children').exists()).toBe(false)
    await wrapper.setProps({active:''});await wrapper.setProps({active:'Loyer/adhoc'})
    expect(wrapper.find('.folder-children').exists()).toBe(true)
    wrapper.unmount()
  })
  it('keeps folder management next to each real folder and creation below the tree',async()=>{
    const wrapper=mount(FolderNavigation,{props,global:{stubs:{FolderManager:true}}})
    await wrapper.get('[aria-label="Expand Loyer"]').trigger('click')
    const row=wrapper.get('.folder-select[title="Loyer/nexim"]').element.closest('.folder-tree-row')!
    const rename=[...row.querySelectorAll('button')].find(button=>button.textContent==='Rename')!
    rename.click();await wrapper.vm.$nextTick()
    expect(wrapper.get('folder-manager-stub').attributes()).toMatchObject({accountid:'work',folder:'Loyer/nexim',action:'rename'})
    expect(wrapper.emitted('select')).toBeUndefined()
    await wrapper.get('[aria-label="Create folder in Work"]').trigger('click')
    expect(wrapper.get('folder-manager-stub').attributes()).toMatchObject({folder:'',action:'create'})
    expect(wrapper.get('.synced-folders').element.compareDocumentPosition(wrapper.get('.folder-create').element)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    wrapper.unmount()
  })
})
