<script setup lang="ts">
import {t} from '../i18n'
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { listContactBook, type Contact, type ContactGroup } from '../api/preferences'
import { useSessionStore } from '../stores/session'
import AppIcon from './AppIcon.vue'
import GoogleContacts from './GoogleContacts.vue'

const props = defineProps<{ target: string; existing: string[]; limit: number }>()
const emit = defineEmits<{ close: []; select: [contacts: Contact[]] }>()
const session = useSessionStore()
const dialog = ref<InstanceType<typeof globalThis.HTMLDialogElement> | null>(null)
const titleID = useId()
const contacts = ref<Contact[]>([])
const groups = ref<ContactGroup[]>([])
const selected = ref<string[]>([])
const query = ref('')
const group = ref('')
const loading = ref(true)
const error = ref('')
const existing = computed(() => new Set(props.existing.map(email => email.trim().toLowerCase())))
const matches = computed(() => contacts.value.filter(contact => (!group.value || contact.group_ids?.includes(group.value)) && `${contact.name} ${contact.email}`.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())).sort((a, b) => a.name.localeCompare(b.name)))
const available = computed(() => matches.value.filter(contact => !existing.value.has(contact.email.toLowerCase())))
const allSelected = computed(() => available.value.length > 0 && available.value.every(contact => selected.value.includes(contact.id)))
let generation = 0

async function load() {
  const request = ++generation
  const epoch = session.vaultEpoch
  loading.value = true; error.value = ''
  try {
    const book = await listContactBook()
    if (request !== generation || !session.unlocked || epoch !== session.vaultEpoch) return
    contacts.value = book.contacts; groups.value = [...book.groups].sort((a, b) => a.name.localeCompare(b.name))
  } catch (value) { if (request === generation) error.value = value instanceof Error ? value.message : 'Could not load contacts.' }
  finally { if (request === generation) loading.value = false }
}
function toggleShown() {
  const ids = new Set(available.value.map(contact => contact.id))
  if (allSelected.value) selected.value = selected.value.filter(id => !ids.has(id))
  else selected.value = [...new Set([...selected.value, ...available.value.map(contact => contact.id)])].slice(0, Math.max(0, props.limit))
}
function toggleContact(id: string) {
  if (selected.value.includes(id)) selected.value = selected.value.filter(value => value !== id)
  else if (selected.value.length < props.limit) selected.value.push(id)
}
function apply() {
  if (!session.unlocked || !selected.value.length || selected.value.length > props.limit) return
  emit('select', contacts.value.filter(contact => selected.value.includes(contact.id) && !existing.value.has(contact.email.toLowerCase())))
}
function applyGoogle(remote: Contact[]) {
  if (!session.unlocked) return
  const local = contacts.value.filter(contact => selected.value.includes(contact.id))
  const seen = new Set(existing.value)
  const combined = [...local, ...remote].filter(contact => {
    const email = contact.email.toLowerCase()
    if (seen.has(email)) return false
    seen.add(email); return true
  })
  if (combined.length && combined.length <= props.limit) emit('select', combined)
}
onMounted(() => { dialog.value?.showModal(); void load() })
onBeforeUnmount(() => { generation++; selected.value = []; contacts.value = []; groups.value = []; dialog.value?.close() })
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="contact-picker" :aria-labelledby="titleID" @cancel.prevent="$emit('close')" @click="($event.target === dialog) && $emit('close')">
      <section @click.stop>
        <header><div><h2 :id="titleID">{{ t("Add contacts to") }} {{ t(target) }}</h2><p>{{ selected.length }} {{ t("selected ·") }} {{ Math.max(0, limit - selected.length) }} {{ t("available recipient slots") }}</p></div><button type="button" class="icon-button" :aria-label="t('Close contact picker')" @click="$emit('close')"><AppIcon name="close" :size="18" /></button></header>
        <div class="contact-picker-filters"><label><span class="sr-only">{{ t("Search contacts") }}</span><input v-model="query" type="search" :placeholder="t('Search name or email')" autofocus maxlength="254"></label><label><span class="sr-only">{{ t("Contact group") }}</span><select v-model="group"><option value="">{{ t("All groups") }}</option><option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option></select></label></div>
        <div class="contact-picker-tools"><button type="button" class="text-button" :disabled="!available.length || !allSelected && selected.length >= limit" @click="toggleShown">{{ allSelected ? t("Deselect shown") : t("Select shown") }}</button><RouterLink to="/contacts" @click="$emit('close')">{{ t("Manage contacts") }}</RouterLink></div>
        <GoogleContacts recipient-mode :existing="[...existing, ...contacts.filter(c => selected.includes(c.id)).map(c => c.email)]" :limit="Math.max(0, limit-selected.length)" @select="applyGoogle" />
        <p v-if="loading" class="contact-picker-message" role="status">{{ t("Loading contacts…") }}</p>
        <p v-else-if="error" class="contact-picker-message error" role="alert">{{ t(error) }} <button type="button" class="text-button" @click="load">{{ t("Try again") }}</button></p>
        <ul v-else-if="matches.length" class="contact-picker-list" :aria-label="t('Choose contacts')">
          <li v-for="contact in matches" :key="contact.id"><label><input type="checkbox" :value="contact.id" :aria-label="t('Select {0}', {'0': contact.name})" :checked="existing.has(contact.email.toLowerCase()) || selected.includes(contact.id)" :disabled="existing.has(contact.email.toLowerCase()) || !selected.includes(contact.id) && selected.length >= limit" @change="toggleContact(contact.id)"><span><strong dir="auto">{{ contact.name }}</strong><small dir="ltr">{{ contact.email }}</small></span><small v-if="existing.has(contact.email.toLowerCase())">{{ t("Already added") }}</small></label></li>
        </ul>
        <p v-else class="contact-picker-message" role="status">{{ query || group ? t("No matching contacts.") : t("No saved contacts yet. Add contacts in the Contacts tab.") }}</p>
        <footer><button type="button" class="secondary" @click="$emit('close')">{{ t("Cancel") }}</button><button type="button" class="primary" :disabled="!selected.length || loading || selected.length > limit" @click="apply">{{ t("Add") }} {{ selected.length || '' }} {{ selected.length === 1 ? t("contact") : t("contacts") }}</button></footer>
      </section>
    </dialog>
  </Teleport>
</template>

<style scoped>
.contact-picker { width: min(520px, calc(100% - 28px)); max-height: calc(100dvh - 28px); overflow: auto; padding: 0; color: var(--ink); background: var(--panel); border: 1px solid var(--line); border-radius: 16px; box-shadow: 0 20px 80px #0005; }
.contact-picker::backdrop { background: #030712a6; backdrop-filter: blur(4px); }
.contact-picker header { display: flex; align-items: center; justify-content: space-between; gap: 15px; padding: 20px; border-bottom: 1px solid var(--line); }
.contact-picker h2 { margin: 0; font-size: 1.1rem; }
.contact-picker header p { margin: 6px 0 0; font-size: .75rem; color: var(--muted); }
.contact-picker-filters { display: grid; grid-template-columns: minmax(0, 1fr) minmax(110px, .6fr); gap: 10px; padding: 16px 20px 0; }
.contact-picker-tools { display: flex; align-items: center; justify-content: space-between; padding: 7px 12px; font-size: .76rem; }
.contact-picker-tools a { padding: 8px; color: var(--accent); }
.contact-picker-list { list-style: none; max-height: min(350px, calc(100dvh - 350px)); overflow: auto; margin: 0; padding: 0 20px; }
.contact-picker-list li { border-top: 1px solid var(--line); }
.contact-picker-list label { display: flex; align-items: center; gap: 12px; min-height: 64px; padding: 10px 0; cursor: pointer; }
.contact-picker-list label > span { display: grid; flex: 1; gap: 4px; min-width: 0; overflow-wrap: anywhere; }
.contact-picker-list strong { font-size: .85rem; font-weight: 550; }
.contact-picker-list small { font-size: .73rem; font-weight: 400; color: var(--muted); }
.contact-picker-message { padding: 20px; font-size: .85rem; }
.contact-picker footer { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 10px; padding: 18px 20px; border-top: 1px solid var(--line); }
@media (max-width: 400px) { .contact-picker-filters { grid-template-columns: minmax(0, 1fr); } .contact-picker-list { max-height: calc(100dvh - 410px); min-height: 100px; } }
</style>
