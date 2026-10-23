<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { MailFolder } from '../api/reading'
import type { FolderMapping } from '../api/accounts'
import { buildFolderTree, folderAncestors, type FolderNode } from '../api/folderTree'
import FolderBranch from './FolderBranch.vue'

const props = defineProps<{ folders: MailFolder[]; mapping?: FolderMapping; accountId: string; accountName: string; active: string }>()
defineEmits<{ select: [path: string] }>()
const nodes = computed(() => buildFolderTree(props.folders,props.mapping))
const expanded = ref(new Map<string,boolean>())
watch(() => props.accountId, () => {expanded.value.clear()})
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
  <ul class="synced-folders" :aria-label="`Folders in ${accountName}`">
    <FolderBranch v-for="node in nodes" :key="node.path" :node="node" :expanded="expanded" :active="active" :depth="0" @select="$emit('select',$event)" @toggle="toggle" />
  </ul>
</template>
