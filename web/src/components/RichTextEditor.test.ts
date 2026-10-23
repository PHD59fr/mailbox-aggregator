import { mount, flushPromises } from '@vue/test-utils'
import { EditorContent, type Editor } from '@tiptap/vue-3'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RichTextEditor from './RichTextEditor.vue'

beforeEach(() => { vi.stubGlobal('requestAnimationFrame', () => 0) })
afterEach(() => { vi.unstubAllGlobals() })

async function openEditor() {
  const wrapper = mount(RichTextEditor, { props: { modelValue: '' } })
  await flushPromises()
  const editor = wrapper.findComponent(EditorContent).props('editor') as Editor
  return { wrapper, editor }
}

describe('rich text email editor', () => {
  it('writes Markdown as HTML with a plain alternative and previews it in a sandbox',async()=>{
    const {wrapper,editor}=await openEditor()
    await wrapper.findAll('.editor-modes button').find(button=>button.text()==='Markdown')!.trigger('click')
    await wrapper.get('#message-markdown').setValue('# Status\n\nHello **team**.\n\n- Ready\n- Testing\n\n| Item | Status |\n| --- | --- |\n| Mail | OK |\n\n```go\nfmt.Println("Hello")\n```')
    await flushPromises()
    expect(editor.getHTML()).toContain('<h1>Status</h1>');expect(editor.getHTML()).toContain('<strong>team</strong>')
    expect(editor.getHTML()).toContain('<table');expect(editor.getHTML()).toContain('<pre>')
    expect(wrapper.emitted('update:text')?.slice(-1)[0]?.[0]).toContain('Hello team.')
    const frame=wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('');expect(frame.attributes('srcdoc')).toContain('<strong>team</strong>')
    expect(frame.attributes('srcdoc')).toContain("default-src 'none'")
    await wrapper.findAll('.editor-modes button').find(button=>button.text()==='Visual')!.trigger('click')
    expect(editor.getHTML()).toContain('<table')
    wrapper.unmount()
  })
  it('preserves visual styling when viewing Markdown without edits and makes preview links inert',async()=>{
    const {wrapper,editor}=await openEditor()
    editor.commands.setContent('<p style="text-align: center"><span style="color: #3b82f6">Styled</span> <a href="https://tracker.test">link</a></p>')
    const original=editor.getHTML()
    const mode=(label:string)=>wrapper.findAll('.editor-modes button').find(button=>button.text()===label)!
    await mode('Markdown').trigger('click');expect((wrapper.get('#message-markdown').element as HTMLTextAreaElement).value).toContain('Styled')
    await mode('Visual').trigger('click');expect(editor.getHTML()).toBe(original)
    await mode('HTML preview').trigger('click')
    const preview=new DOMParser().parseFromString(wrapper.get('iframe').attributes('srcdoc')!,'text/html')
    expect(preview.querySelector('a')?.getAttribute('href')).toBeNull()
    expect(preview.querySelector('span')?.className).toContain('email-color-0')
    expect(preview.querySelector('p')?.className).toContain('email-align-center')
    expect(preview.querySelector('[style]')).toBeNull();wrapper.unmount()
  })
  it('supports headings, sizes, colors, highlights, alignment and clearing formatting', async () => {
    const { wrapper, editor } = await openEditor()
    editor.commands.setContent('<p>A formatted message</p>')
    editor.commands.selectAll()
    await wrapper.find('[aria-label="Text style"]').setValue('h2')
    await wrapper.find('[aria-label="Font size"]').setValue('20px')
    await wrapper.find('[aria-label="Text alignment"]').setValue('center')
    await wrapper.find('[aria-label="Strikethrough"]').trigger('click')
    await wrapper.find('[aria-label="Text color"]').trigger('click')
    await wrapper.find('[aria-label="Text color Blue"]').trigger('click')
    await wrapper.find('[aria-label="Highlight text"]').trigger('click')
    await wrapper.find('[aria-label="Highlight Amber"]').trigger('click')
    expect(editor.getHTML()).toContain('<h2')
    expect(editor.getHTML()).toContain('text-align: center')
    expect(editor.getHTML()).toContain('font-size: 20px')
    expect(editor.getHTML()).toContain('background-color:')
    expect(editor.getHTML()).toContain('<s>')
    await wrapper.find('[aria-label="Clear formatting"]').trigger('click')
    expect(editor.getText().trim()).toBe('A formatted message')
    expect(editor.getHTML()).not.toMatch(/style=|<h2|<s>/)
    wrapper.unmount()
  })

  it('emits formatted HTML and its plain-text alternative', async () => {
    const { wrapper, editor } = await openEditor()
    editor.commands.setContent('<p>Hello <strong>team</strong></p><ul><li><p>First item</p></li></ul>')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.slice(-1)[0]?.[0]).toContain('<strong>team</strong>')
    expect(wrapper.emitted('update:text')?.slice(-1)[0]?.[0]).toContain('Hello team')
    expect(wrapper.emitted('update:text')?.slice(-1)[0]?.[0]).toContain('First item')
    editor.commands.selectAll()
    await wrapper.find('[aria-label="Italic"]').trigger('click')
    expect(editor.getHTML()).toContain('<em>')
    wrapper.unmount()
  })

  it('validates links instead of accepting executable URLs', async () => {
    const { wrapper, editor } = await openEditor()
    editor.commands.setContent('<p>Read more</p>')
    editor.commands.selectAll()
    await wrapper.find('[aria-label="Insert link"]').trigger('click')
    await wrapper.find('#message-link').setValue('javascript:alert(1)')
    await wrapper.find('.editor-link-panel .secondary').trigger('click')
    expect(wrapper.find('[role="alert"]').text()).toContain('valid')
    expect(editor.getHTML()).not.toContain('javascript:')
    await wrapper.find('#message-link').setValue('https://example.test/help')
    await wrapper.find('.editor-link-panel .secondary').trigger('click')
    expect(editor.getHTML()).toContain('href="https://example.test/help"')
    wrapper.unmount()
  })

  it('disables the content and formatting while sending and clears after acceptance', async () => {
    const { wrapper, editor } = await openEditor()
    editor.commands.setContent('<p>A draft</p>')
    await wrapper.setProps({ modelValue: '<p>A draft</p>', disabled: true })
    expect(wrapper.find('[role="textbox"]').attributes('contenteditable')).toBe('false')
    expect(wrapper.find('[aria-label="Bold"]').attributes()).toHaveProperty('disabled')
    await wrapper.setProps({ modelValue: '', disabled: false })
    expect(editor.isEmpty).toBe(true)
    wrapper.unmount()
  })
})
