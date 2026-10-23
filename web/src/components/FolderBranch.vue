<script setup lang="ts">
import {t} from '../i18n'
import { computed, nextTick, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import type { FolderNode } from '../api/folderTree'
import type { FolderAction } from '../api/reading'

const props = defineProps<{ node: FolderNode; expanded: Map<string, boolean>; active: string; depth: number; menuGroup: string }>()
const emit = defineEmits<{ select: [path: string]; toggle: [node: FolderNode]; manage: [action: FolderAction, path: string] }>()
const open = computed(() => props.expanded.get(props.node.path) ?? props.node.hasStandardChildren)
const actions = ref<InstanceType<typeof globalThis.HTMLDetailsElement> | null>(null)
const menu = ref<InstanceType<typeof globalThis.HTMLElement> | null>(null)
const menuAbove = ref(false)
async function positionMenu() {
  if (!actions.value?.open) return
  menuAbove.value = false
  await nextTick()
  if (menu.value && menu.value.getBoundingClientRect().bottom > globalThis.innerHeight - 96) menuAbove.value = true
}
function closeMenu() { if (actions.value) actions.value.open = false; actions.value?.querySelector('summary')?.focus() }
function manage(action: FolderAction) { closeMenu(); emit('manage', action, props.node.path) }
</script>

<template>
  <li>
    <div class="folder-tree-row">
      <button v-if="node.children.length" type="button" class="folder-expander" :class="{ expanded: open }" :aria-expanded="open" :aria-label="`${open ? t('Collapse') : t('Expand')} ${node.label}`" @click="$emit('toggle', node)"><AppIcon name="chevron" :size="13" /></button>
      <span v-else class="folder-expander-spacer" aria-hidden="true" />
      <button v-if="node.folder?.selectable" type="button" class="folder-select" :class="{active: active === node.path}" :aria-pressed="active === node.path" :title="node.folder.error || node.path" @click="$emit('select',node.path)"><AppIcon :name="node.folder.icon" :size="15" /><span>{{ node.folder?.role ? t(node.label) : node.label }}</span><b v-if="node.folder.unseen">{{ node.folder.unseen }}</b></button>
      <span v-else class="folder-container" :title="node.path"><AppIcon name="folder" :size="15" /><span>{{ node.folder?.role ? t(node.label) : node.label }}</span></span>
      <details v-if="node.folder" ref="actions" class="folder-actions" :name="menuGroup" @toggle="positionMenu" @keydown.esc.prevent.stop="closeMenu">
        <summary class="folder-menu-trigger" :aria-label="t('Actions for {0}', {'0': node.path})" :title="t('Actions for {0}', {'0': node.label})"><span aria-hidden="true">⋯</span></summary>
        <div ref="menu" class="folder-action-menu" :class="{ 'opens-above': menuAbove }" role="group" :aria-label="t('Actions for {0}', {'0': node.path})">
          <button type="button" @click="manage('rename')">{{ t("Rename") }}</button>
          <button type="button" @click="manage('subscribe')">{{ t("Subscribe") }}</button>
          <button type="button" @click="manage('unsubscribe')">{{ t("Unsubscribe") }}</button>
          <button type="button" class="folder-delete" @click="manage('delete')">{{ t("Delete folder") }}</button>
        </div>
      </details>
    </div>
    <ul v-if="node.children.length && open" class="folder-children" :class="{'folder-children-flat':depth >= 5}" :aria-label="t('Subfolders of {0}', {'0': node.label})">
      <FolderBranch v-for="child in node.children" :key="child.path" :node="child" :expanded="expanded" :active="active" :depth="depth+1" :menu-group="menuGroup" @select="$emit('select',$event)" @toggle="$emit('toggle',$event)" @manage="(action,path) => $emit('manage',action,path)" />
    </ul>
  </li>
</template>
