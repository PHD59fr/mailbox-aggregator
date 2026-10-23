<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import type { FolderNode } from '../api/folderTree'

const props = defineProps<{ node: FolderNode; expanded: Map<string, boolean>; active: string; depth: number }>()
defineEmits<{ select: [path: string]; toggle: [node: FolderNode] }>()
const open = computed(() => props.expanded.get(props.node.path) ?? props.node.hasStandardChildren)
</script>

<template>
  <li>
    <div class="folder-tree-row">
      <button v-if="node.children.length" type="button" class="folder-expander" :class="{ expanded: open }" :aria-expanded="open" :aria-label="`${open ? 'Collapse' : 'Expand'} ${node.label}`" @click="$emit('toggle', node)"><AppIcon name="chevron" :size="13" /></button>
      <span v-else class="folder-expander-spacer" aria-hidden="true" />
      <button v-if="node.folder?.selectable" type="button" class="folder-select" :class="{active: active === node.path}" :aria-pressed="active === node.path" :title="node.folder.error || node.path" @click="$emit('select',node.path)"><AppIcon :name="node.folder.icon" :size="15" /><span>{{ node.label }}</span><b v-if="node.folder.unseen">{{ node.folder.unseen }}</b></button>
      <span v-else class="folder-container" :title="node.path"><AppIcon name="folder" :size="15" /><span>{{ node.label }}</span></span>
    </div>
    <ul v-if="node.children.length && open" class="folder-children" :class="{'folder-children-flat':depth >= 5}" :aria-label="`Subfolders of ${node.label}`">
      <FolderBranch v-for="child in node.children" :key="child.path" :node="child" :expanded="expanded" :active="active" :depth="depth+1" @select="$emit('select',$event)" @toggle="$emit('toggle',$event)" />
    </ul>
  </li>
</template>
