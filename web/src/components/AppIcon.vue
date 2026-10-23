<script setup lang="ts">
type IconName = 'mail' | 'inbox' | 'send' | 'contacts' | 'contact-add' | 'copy' | 'settings' | 'logout' | 'lock' | 'unlock' | 'search' | 'plus' | 'arrow' | 'close' | 'refresh' | 'shield' | 'chevron' | 'check' | 'paperclip' | 'list' | 'ordered-list' | 'link' | 'undo' | 'redo' | 'trash' | 'edit' | 'quote' | 'clear-format' | 'reply' | 'reply-all' | 'forward' | 'redirect' | 'code' | 'folder' | 'filter'
defineProps<{ name: IconName; size?: number }>()
const paths: Record<IconName, string[]> = {
  copy: ['M9 8h11v13H9Z', 'M15 8V3H4v13h5'],
  contacts: ['M5 3h15v18H5Z', 'M3 7h4M3 12h4M3 17h4', 'M15 9a2.5 2.5 0 1 1-5 0 2.5 2.5 0 0 1 5 0Z', 'M9 17v-1a3.5 3.5 0 0 1 7 0v1'],
  'contact-add': ['M14 7a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z', 'M4 20v-2a6 6 0 0 1 10-4', 'M18 14v8', 'M14 18h8'],
  filter: ['M4 6h16M7 12h10M10 18h4', 'M8 4v4M16 10v4M12 16v4'],
  reply: ['m9 5-6 6 6 6', 'M3 11h10a7 7 0 0 1 7 7'],
  'reply-all': ['m10 5-6 6 6 6', 'm5 5-5 6 5 6', 'M4 11h10a7 7 0 0 1 7 7'],
  forward: ['m15 5 6 6-6 6', 'M21 11H11a7 7 0 0 0-7 7'],
  redirect: ['M3 17h5a4 4 0 0 0 4-4V9a4 4 0 0 1 4-4h5', 'm17 1 4 4-4 4'],
  code: ['m8 5-6 7 6 7', 'm16 5 6 7-6 7', 'm14 3-4 18'],
  folder: ['M3 7V4h6l3 3h9v13H3Z'],
  mail: ['M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1Z', 'm3 6 9 7 9-7'],
  inbox: ['m4 4-2 10v5a1 1 0 0 0 1 1h18a1 1 0 0 0 1-1v-5L20 4Z', 'M2 14h6l2 3h4l2-3h6'],
  send: ['m21 3-7 18-4-7-7-4Z', 'm10 14 5-5'],
  settings: ['m9 3-.6 2.3-2 .9-2.1-.7-2 3.5 1.6 1.6-.1 2.3-1.5 1.7 2 3.4 2.2-.6 2 .9.5 2.2h4l.6-2.3 2-.9 2.1.7 2-3.5-1.6-1.6.1-2.3 1.5-1.7-2-3.4-2.2.6-2-.9L13 3Z', 'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z'],
  logout: ['M9 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h4', 'M9 12h12', 'm17 8 4 4-4 4'],
  lock: ['M6 10h12a1 1 0 0 1 1 1v9H5v-9a1 1 0 0 1 1-1Z', 'M8 10V6a4 4 0 0 1 8 0v4', 'M12 14v2'],
  unlock: ['M6 10h12a1 1 0 0 1 1 1v9H5v-9a1 1 0 0 1 1-1Z', 'M8 10V6a4 4 0 0 1 7.5-2', 'M12 14v2'],
  search: ['M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z', 'm15 15 6 6'],
  plus: ['M12 5v14', 'M5 12h14'],
  arrow: ['M4 12h16', 'm14 6 6 6-6 6'],
  close: ['m6 6 12 12', 'M18 6 6 18'],
  refresh: ['M20 7v5h-5', 'M4 17v-5h5', 'M6 7a7 7 0 0 1 12-1l2 3', 'M18 17a7 7 0 0 1-12 1l-2-3'],
  shield: ['M12 2 3 6v6c0 5 9 10 9 10s9-5 9-10V6Z', 'm8 12 3 3 5-6'],
  chevron: ['m9 5 7 7-7 7'],
  check: ['m5 12 4 4L19 6'],
  paperclip: ['m9 17 9-9a3 3 0 0 0-4-4L4 14a5 5 0 0 0 7 7L21 11', 'm7 15 9-9'],
  list: ['M9 6h12M9 12h12M9 18h12', 'M3 6h1M3 12h1M3 18h1'],
  'ordered-list': ['M10 6h11M10 12h11M10 18h11', 'm3 4 1-1v5M3 8h3', 'M3 14c0-2 3-2 3 0 0 1-3 2-3 4h3'],
  link: ['m10 13 4-4', 'm8 15-2 2a3.5 3.5 0 0 1-5-5l4-4a3.5 3.5 0 0 1 5 0', 'm14 9 2-2a3.5 3.5 0 0 1 5 5l-4 4a3.5 3.5 0 0 1-5 0'],
  undo: ['M4 10h10a6 6 0 0 1 0 12', 'm8 6-4 4 4 4'],
  redo: ['M20 10H10a6 6 0 0 0 0 12', 'm16 6 4 4-4 4'],
  trash: ['M3 6h18', 'm5 6 1 15h12l1-15', 'M9 6V3h6v3', 'M10 10v7M14 10v7'],
  edit: ['m16 3 5 5-12 12-6 1 1-6Z', 'm14 5 5 5'],
  quote: ['M4 5h6v8H4Z', 'M4 13v2a4 4 0 0 0 4 4', 'M14 5h6v8h-6Z', 'M14 13v2a4 4 0 0 0 4 4'],
  'clear-format': ['M3 4h14M10 4v16M6 20h8', 'm15 13 7 7M22 13l-7 7'],
}
</script>

<template>
  <svg :width="size || 20" :height="size || 20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="app-icon">
    <path v-for="(path, index) in paths[name]" :key="index" :d="path" />
  </svg>
</template>
