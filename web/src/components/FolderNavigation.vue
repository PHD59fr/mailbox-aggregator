<script setup lang="ts">
import {t} from '../i18n'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import type { FolderAction, MailFolder } from '../api/reading'
import type { FolderMapping } from '../api/accounts'
import { buildFolderTree, folderAncestors, type FolderNode } from '../api/folderTree'
import FolderBranch from './FolderBranch.vue'
import FolderManager from './FolderManager.vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ folders: MailFolder[]; mapping?: FolderMapping; accountId: string; accountName: string; active: string }>()
const emit = defineEmits<{ select: [path: string]; changed: [action: FolderAction, name: string, newName: string] }>()
const root = ref<InstanceType<typeof globalThis.HTMLElement> | null>(null)
const management = ref<{ action: FolderAction; folder: string } | null>(null)
function closeMenus(event?: InstanceType<typeof globalThis.Event>) {
  for (const details of root.value?.querySelectorAll<InstanceType<typeof globalThis.HTMLDetailsElement>>('.folder-actions[open]') || []) {
    if (!event || !(event.target instanceof globalThis.Node) || !details.contains(event.target)) details.open = false
  }
}
function manage(action: FolderAction, folder: string) { closeMenus(); management.value = { action, folder } }
function changed(action: FolderAction, name: string, newName: string) { management.value = null; emit('changed', action, name, newName) }
onMounted(() => globalThis.document.addEventListener('pointerdown', closeMenus))
onUnmounted(() => globalThis.document.removeEventListener('pointerdown', closeMenus))
const nodes = computed(() => buildFolderTree(props.folders,props.mapping))
const expanded = ref(new Map<string,boolean>())
watch(() => props.accountId, () => {expanded.value.clear(); management.value = null; closeMenus()})
watch([() => props.active, () => props.folders.map(folder => `${folder.delimiter}:${folder.name}`).join('\n')], () => {
  const paths = new Set<string>()
  function collect(branch: FolderNode[]) {for(const node of branch){paths.add(node.path);collect(node.children)}}
  collect(nodes.value)
  for(const path of expanded.value.keys())if(!paths.has(path))expanded.value.delete(path)
  for(const path of folderAncestors(nodes.value,props.active) || [])expanded.value.set(path,true)
}, {immediate:true})
function toggle(node: FolderNode) {
  expanded.value.set(node.path,!(expanded.value.get(node.path) ?? node.hasStandardChildren))
}
</script>

<template>
  <div ref="root" class="folder-navigation">
    <ul class="synced-folders" :aria-label="t('Folders in {0}', {'0': accountName})">
      <FolderBranch v-for="node in nodes" :key="node.path" :node="node" :expanded="expanded" :active="active" :depth="0" :menu-group="`folder-actions-${accountId}`" @select="$emit('select',$event)" @toggle="toggle" @manage="manage" />
    </ul>
    <button type="button" class="folder-create" :aria-label="t('Create folder in {0}', {'0': accountName})" @click="manage('create', '')"><AppIcon name="plus" :size="16" />{{ t("New folder") }}</button>
    <FolderManager v-if="management" :accountID="accountId" :account-name="accountName" :action="management.action" :folder="management.folder" @close="management = null" @changed="changed" />
  </div>
</template>
