<script setup lang="ts">
import {t} from '../i18n'
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { manageFolder, type FolderAction } from '../api/reading'
import { useSessionStore } from '../stores/session'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ accountID: string; accountName: string; action: FolderAction; folder: string }>()
const emit = defineEmits<{ close: []; changed: [action: FolderAction, name: string, newName: string] }>()
const session = useSessionStore()
const dialog = ref<InstanceType<typeof globalThis.HTMLDialogElement> | null>(null)
const titleID = useId()
const name = ref(props.action === 'create' ? '' : props.folder)
const newName = ref(props.action === 'rename' ? props.folder : '')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')
const controller = new globalThis.AbortController()
const titles = { create: 'New folder', rename: 'Rename folder', delete: 'Delete folder', subscribe: 'Subscribe to folder', unsubscribe: 'Unsubscribe from folder' }
const labels = { create: 'Create folder', rename: 'Rename folder', delete: 'Delete folder', subscribe: 'Subscribe', unsubscribe: 'Unsubscribe' }
const valid = computed(() => !!name.value.trim() && (props.action !== 'rename' || !!newName.value.trim() && newName.value !== name.value) && (props.action !== 'delete' || confirmation.value === name.value) && !((props.action === 'delete' || props.action === 'rename') && name.value.toUpperCase() === 'INBOX'))

function close() { if (!busy.value) emit('close') }
watch([() => props.accountID, () => session.vaultEpoch], () => { controller.abort(); emit('close') })
onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => { controller.abort(); dialog.value?.close() })
async function submit() {
  if (busy.value || !session.unlocked || !valid.value) return
  const epoch = session.vaultEpoch
  busy.value = true
  error.value = ''
  try {
    await manageFolder(props.accountID, props.action, name.value, newName.value, confirmation.value, controller.signal)
    if (controller.signal.aborted || epoch !== session.vaultEpoch) return
    session.showNotice(props.action === 'rename' || props.action === 'delete' ? 'Folder action confirmed. Check folder mappings in Settings if needed.' : 'Folder action confirmed.')
    emit('changed', props.action, name.value, newName.value)
  } catch (value) {
    if (!controller.signal.aborted && epoch === session.vaultEpoch) error.value = value instanceof Error ? value.message : 'Could not confirm the folder action. Refresh before retrying.'
  } finally { busy.value = false }
}
</script>
<template>
  <Teleport to="body">
    <dialog ref="dialog" class="folder-dialog" :aria-labelledby="titleID" @cancel.prevent="close" @click="($event.target === dialog) && close()">
      <section @click.stop>
        <header><div><h2 :id="titleID">{{ t(titles[action]) }}</h2><p>{{ accountName }}</p></div><button type="button" class="icon-button" :aria-label="t('Close folder dialog')" :disabled="busy" @click="close"><AppIcon name="close" :size="18" /></button></header>
        <form @submit.prevent="submit"><fieldset :disabled="busy">
          <label v-if="action === 'create'">{{ t("Folder name") }}<input v-model="name" autofocus required maxlength="1024" autocomplete="off"></label>
          <p v-else class="folder-dialog-path">{{ folder }}</p>
          <label v-if="action === 'rename'">{{ t("New name") }}<input v-model="newName" autofocus required maxlength="1024" autocomplete="off"></label>
          <template v-if="action === 'delete'"><p class="error">{{ t("Deleting this folder can permanently delete its messages on the provider.") }}</p><label>{{ t("Type the exact folder name to confirm") }}<input v-model="confirmation" autofocus autocomplete="off" required></label></template>
          <p v-if="error" class="error" role="alert">{{ t(error) }}</p>
          <div class="folder-dialog-actions"><button type="button" class="secondary" @click="close">{{ t("Cancel") }}</button><button type="submit" :class="action === 'delete' ? 'danger-button' : 'primary'" :disabled="!valid || busy">{{ busy ? t("Working…") : t(labels[action]) }}</button></div>
        </fieldset></form>
      </section>
    </dialog>
  </Teleport>
</template>
