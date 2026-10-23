<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import { TextStyle, Color, BackgroundColor, FontSize } from '@tiptap/extension-text-style'
import TextAlign from '@tiptap/extension-text-align'
import { Markdown } from '@tiptap/markdown'
import { TableKit } from '@tiptap/extension-table'
import { composePreviewDocument } from '../api/composePreview'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ modelValue: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; 'update:text': [value: string]; 'update:valid':[value:boolean] }>()
const mode=ref<'rich'|'markdown'|'preview'>('rich')
const markdown=ref('')
const previewHTML=ref('')
const markdownError=ref('')
const previewDocument=computed(()=>composePreviewDocument(previewHTML.value))
const editingLink = ref(false)
const linkURL = ref('')
const linkError = ref('')
const colorMode = ref<'text' | 'highlight' | null>(null)
const colors = [
  { name: 'Blue', text: '#3b82f6', highlight: '#bfdbfe' },
  { name: 'Purple', text: '#a855f7', highlight: '#e9d5ff' },
  { name: 'Rose', text: '#e11d48', highlight: '#fecdd3' },
  { name: 'Amber', text: '#d97706', highlight: '#fef08a' },
  { name: 'Green', text: '#10b981', highlight: '#bbf7d0' },
  { name: 'Gray', text: '#64748b', highlight: '#e2e8f0' },
]
const editor = useEditor({
  extensions: [StarterKit.configure({ heading: { levels: [1, 2, 3, 4, 5, 6] },
    link: { openOnClick: false, autolink: false, protocols: ['http', 'https', 'mailto'] } }),
    TextStyle, Color, BackgroundColor, FontSize, TextAlign.configure({ types: ['heading', 'paragraph'] }), Markdown.configure({markedOptions:{gfm:true,breaks:true}}), TableKit.configure({table:{resizable:false}})],
  content: props.modelValue,
  editable: !props.disabled,
  editorProps: { attributes: { role: 'textbox', 'aria-label': 'Message', 'aria-multiline': 'true', 'data-placeholder': 'Write your message…' } },
  onCreate:({editor})=>{previewHTML.value=editor.isEmpty ? '' : editor.getHTML()},
  onUpdate: ({ editor }) => {
    previewHTML.value=editor.isEmpty ? '' : editor.getHTML()
    emit('update:modelValue', previewHTML.value)
    emit('update:text', editor.getText({ blockSeparator: '\n\n' }))
  },
})
function setMode(value:'rich'|'markdown'|'preview') {
  if (props.disabled || markdownError.value) return
  if(value==='markdown' && mode.value!=='markdown') markdown.value=editor.value?.getMarkdown() || ''
  mode.value=value;editingLink.value=false;colorMode.value=null
}
function editMarkdown(event:{target:unknown}) {
  markdown.value=(event.target as {value:string}).value
  try {
    editor.value?.commands.setContent(markdown.value,{contentType:'markdown'})
    markdownError.value=''
    emit('update:valid',true)
  } catch {markdownError.value='This Markdown could not be formatted. Check its structure before switching views.';emit('update:valid',false)}
}
function blockStyle(event: { target: unknown }) {
  const value = (event.target as { value: string }).value
  if (value === 'h2' || value === 'h3') editor.value?.chain().focus().setHeading({ level: value === 'h2' ? 2 : 3 }).run()
  else editor.value?.chain().focus().setParagraph().run()
}
function fontSize(event: { target: unknown }) {
  const value = (event.target as { value: string }).value
  if (['12px', '16px', '20px', '24px'].includes(value)) editor.value?.chain().focus().setFontSize(value).run()
  else editor.value?.chain().focus().unsetFontSize().run()
}
function alignment(event: { target: unknown }) {
  const value = (event.target as { value: string }).value
  if (['left', 'center', 'right', 'justify'].includes(value)) editor.value?.chain().focus().setTextAlign(value).run()
}
function applyColor(value?: string) {
  const chain = editor.value?.chain().focus()
  if (colorMode.value === 'text') {
    if (value) chain?.setColor(value).run()
    else chain?.unsetColor().run()
  } else {
    if (value) chain?.setBackgroundColor(value).run()
    else chain?.unsetBackgroundColor().run()
  }
  colorMode.value = null
}
watch(() => props.disabled, value => { editor.value?.setEditable(!value) })
watch(() => props.modelValue, value => {
  if (editor.value && value !== editor.value.getHTML() && !(value === '' && editor.value.isEmpty)) {
    editor.value.commands.setContent(value, { emitUpdate: false })
    previewHTML.value=editor.value.isEmpty ? '' : editor.value.getHTML()
    if(mode.value==='markdown')markdown.value=editor.value.getMarkdown()
  }
})

function editLink() {
  linkURL.value = editor.value?.getAttributes('link').href || 'https://'
  linkError.value = ''
  editingLink.value = !editingLink.value
}
function applyLink() {
  try {
    const url = new globalThis.URL(linkURL.value.trim())
    if (!['https:', 'http:', 'mailto:'].includes(url.protocol)) throw new Error('Invalid protocol')
    editor.value?.chain().focus().extendMarkRange('link').setLink({ href: url.href }).run()
    editingLink.value = false
  } catch {
    linkError.value = 'Enter a valid https://, http:// or mailto: link.'
  }
}
</script>

<template>
  <div class="rich-editor">
    <div class="editor-modes" role="group" aria-label="Message editor mode"><button type="button" :aria-pressed="mode==='rich'" :disabled="disabled || !!markdownError" @click="setMode('rich')">Visual</button><button type="button" :aria-pressed="mode==='markdown'" :disabled="disabled" @click="setMode('markdown')"><AppIcon name="code" :size="15" />Markdown</button><button type="button" :aria-pressed="mode==='preview'" :disabled="disabled || !!markdownError" @click="setMode('preview')">HTML preview</button></div>
    <div v-show="mode==='rich'">
    <div v-if="editor" class="format-toolbar" role="toolbar" aria-label="Text formatting">
      <select class="format-style" aria-label="Text style" :value="editor.isActive('heading', { level: 2 }) ? 'h2' : editor.isActive('heading', { level: 3 }) ? 'h3' : 'p'" :disabled="disabled" @change="blockStyle"><option value="p">Normal text</option><option value="h2">Heading</option><option value="h3">Subheading</option></select>
      <select class="format-size" aria-label="Font size" :value="editor.getAttributes('textStyle').fontSize || ''" :disabled="disabled" @change="fontSize"><option value="">Size</option><option value="12px">Small</option><option value="16px">Normal</option><option value="20px">Large</option><option value="24px">Huge</option></select>
      <span class="format-divider"></span>
      <button type="button" aria-label="Bold" title="Bold (Ctrl+B)" :aria-pressed="editor.isActive('bold')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleBold().run()"><strong>B</strong></button>
      <button type="button" aria-label="Italic" title="Italic (Ctrl+I)" :aria-pressed="editor.isActive('italic')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleItalic().run()"><em>I</em></button>
      <button type="button" aria-label="Underline" title="Underline (Ctrl+U)" :aria-pressed="editor.isActive('underline')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleUnderline().run()"><u>U</u></button>
      <button type="button" aria-label="Strikethrough" title="Strikethrough" :aria-pressed="editor.isActive('strike')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleStrike().run()"><s>S</s></button>
      <button type="button" aria-label="Text color" title="Text color" :aria-expanded="colorMode === 'text'" :disabled="disabled" @mousedown.prevent @click="colorMode = colorMode === 'text' ? null : 'text'"><span class="format-color-letter">A</span></button>
      <button type="button" aria-label="Highlight text" title="Highlight text" :aria-expanded="colorMode === 'highlight'" :disabled="disabled" @mousedown.prevent @click="colorMode = colorMode === 'highlight' ? null : 'highlight'"><mark>A</mark></button>
      <span class="format-divider"></span>
      <button type="button" aria-label="Bullet list" title="Bullet list" :aria-pressed="editor.isActive('bulletList')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleBulletList().run()"><AppIcon name="list" :size="18" /></button>
      <button type="button" aria-label="Numbered list" title="Numbered list" :aria-pressed="editor.isActive('orderedList')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleOrderedList().run()"><AppIcon name="ordered-list" :size="18" /></button>
      <button type="button" aria-label="Quote" title="Quote" :aria-pressed="editor.isActive('blockquote')" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().toggleBlockquote().run()"><AppIcon name="quote" :size="18" /></button>
      <select class="format-align" aria-label="Text alignment" :value="editor.getAttributes('paragraph').textAlign || editor.getAttributes('heading').textAlign || 'left'" :disabled="disabled" @change="alignment"><option value="left">Left</option><option value="center">Center</option><option value="right">Right</option><option value="justify">Justify</option></select>
      <button type="button" aria-label="Insert link" title="Insert link" :aria-pressed="editor.isActive('link')" :disabled="disabled" @mousedown.prevent @click="editLink"><AppIcon name="link" :size="18" /></button>
      <span class="format-divider"></span>
      <button type="button" aria-label="Undo" title="Undo" :disabled="disabled || !editor.can().undo()" @mousedown.prevent @click="editor.chain().focus().undo().run()"><AppIcon name="undo" :size="18" /></button>
      <button type="button" aria-label="Redo" title="Redo" :disabled="disabled || !editor.can().redo()" @mousedown.prevent @click="editor.chain().focus().redo().run()"><AppIcon name="redo" :size="18" /></button>
      <button type="button" aria-label="Clear formatting" title="Clear formatting" :disabled="disabled" @mousedown.prevent @click="editor.chain().focus().unsetAllMarks().clearNodes().unsetTextAlign().run()"><AppIcon name="clear-format" :size="18" /></button>
    </div>
    <div v-if="colorMode" class="editor-colors-panel" role="group" :aria-label="colorMode === 'text' ? 'Text colors' : 'Highlight colors'">
      <span>{{ colorMode === 'text' ? 'Text color' : 'Highlight' }}</span>
      <button v-for="color in colors" :key="color.name" type="button" class="color-swatch" :aria-label="`${colorMode === 'text' ? 'Text color' : 'Highlight'} ${color.name}`" :title="color.name" :style="{ backgroundColor: colorMode === 'text' ? color.text : color.highlight }" :disabled="disabled" @mousedown.prevent @click="applyColor(colorMode === 'text' ? color.text : color.highlight)"></button>
      <button type="button" class="color-reset" :disabled="disabled" @mousedown.prevent @click="applyColor()">Reset</button>
      <button type="button" class="icon-button" aria-label="Close colors" @click="colorMode = null"><AppIcon name="close" :size="15" /></button>
    </div>
    <div v-if="editingLink" class="editor-link-panel">
      <label for="message-link" class="sr-only">Link URL</label><input id="message-link" v-model="linkURL" inputmode="url" placeholder="https://example.com" :disabled="disabled" @keydown.enter.prevent="applyLink">
      <button type="button" class="secondary" :disabled="disabled" @click="applyLink">Apply</button>
      <button type="button" class="icon-button" aria-label="Remove link" :disabled="disabled" @click="editor?.chain().focus().extendMarkRange('link').unsetLink().run(); editingLink = false"><AppIcon name="close" :size="16" /></button>
      <p v-if="linkError" class="error" role="alert">{{ linkError }}</p>
    </div>
    <EditorContent :editor="editor" />
    </div>
    <div v-if="mode==='markdown'" class="markdown-workspace">
      <div class="markdown-source"><label for="message-markdown" class="section-label">Markdown</label><textarea id="message-markdown" :value="markdown" :disabled="disabled" maxlength="1048576" spellcheck="false" placeholder="# Your heading&#10;&#10;Write **bold**, *italic*, lists and links…" @input="editMarkdown" /><details class="markdown-help"><summary>Markdown help</summary><pre># Heading
**Bold** · *Italic* · ~~Strikethrough~~
[Link](https://example.com)
- List item
&gt; Quote
`inline code` or fenced code blocks

| Column | Column |
| --- | --- |
| Value | Value |</pre><p>Editing Markdown simplifies custom colors and alignment. Switching views without editing keeps your visual formatting.</p></details><p v-if="markdownError" class="error" role="alert">{{ markdownError }}</p></div>
      <div class="markdown-preview"><p class="section-label">Live HTML preview</p><iframe title="Live HTML preview" sandbox="" referrerpolicy="no-referrer" :srcdoc="previewDocument" /></div>
    </div>
    <div v-else-if="mode==='preview'" class="compose-html-preview"><iframe title="HTML message preview" sandbox="" referrerpolicy="no-referrer" :srcdoc="previewDocument" /></div>
  </div>
</template>
