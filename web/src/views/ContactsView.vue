<script setup lang="ts">
import {t, formatNumber, plural} from '../i18n'
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import AppHeader from '../components/AppHeader.vue'
import AppIcon from '../components/AppIcon.vue'
import VaultUnlock from '../components/VaultUnlock.vue'
import ContactTransfer from '../components/ContactTransfer.vue'
import GoogleContacts from '../components/GoogleContacts.vue'
import RecipientHistorySettings from '../components/RecipientHistorySettings.vue'
import ExternalContacts from '../components/ExternalContacts.vue'
import { deleteContact, deleteContactGroup, listContactBook, saveContact, saveContactGroup, type Contact, type ContactGroup } from '../api/preferences'
import { useSessionStore } from '../stores/session'

const session = useSessionStore()
const contacts = ref<Contact[]>([])
const groups = ref<ContactGroup[]>([])
const query = ref('')
// null selects all contacts; an empty string selects ungrouped contacts.
const selectedGroup = ref<string | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const formError = ref('')
const modal = ref<'contact' | 'group' | 'delete-contact' | 'delete-group' | null>(null)
const dialog = ref<InstanceType<typeof globalThis.HTMLDialogElement> | null>(null)
const titleID = useId()
const contactID = ref('')
const contactName = ref('')
const contactEmail = ref('')
const memberships = ref<string[]>([])
const groupID = ref('')
const groupName = ref('')
let generation = 0

const sortedGroups = computed(() => [...groups.value].sort((a, b) => a.name.localeCompare(b.name)))
const groupNames = computed(() => new Map(groups.value.map(group => [group.id, group.name])))
const groupCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const contact of contacts.value) for (const id of contact.group_ids || []) counts.set(id, (counts.get(id) || 0) + 1)
  return counts
})
const ungrouped = computed(() => contacts.value.filter(contact => !contact.group_ids?.length).length)
const selectionName = computed(() => selectedGroup.value === null ? t('All contacts') : selectedGroup.value === '' ? t('Ungrouped') : groupNames.value.get(selectedGroup.value) || t('Group'))
const matching = computed(() => contacts.value.filter(contact => {
  const inGroup = selectedGroup.value === null || (selectedGroup.value === '' ? !contact.group_ids?.length : contact.group_ids?.includes(selectedGroup.value))
  return inGroup && `${contact.name} ${contact.email}`.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())
}).sort((a, b) => a.name.localeCompare(b.name)))
const modalTitle = computed(() => modal.value === 'contact' ? contactID.value ? 'Edit contact' : 'New contact' : modal.value === 'group' ? groupID.value ? 'Edit group' : 'New group' : modal.value === 'delete-contact' ? 'Delete contact' : 'Delete group')

function current() {
  const request = generation
  const user = session.userID
  const epoch = session.vaultEpoch
  return () => request === generation && session.unlocked && session.userID === user && session.vaultEpoch === epoch
}
async function load() {
  generation++
  const active = current()
  loading.value = true
  error.value = ''
  try {
    const book = await listContactBook()
    if (!active()) return
    contacts.value = book.contacts
    groups.value = book.groups
    if (selectedGroup.value && !book.groups.some(group => group.id === selectedGroup.value)) selectedGroup.value = null
  } catch (value) { if (active()) error.value = value instanceof Error ? value.message : 'Could not load contacts.' }
  finally { if (active()) loading.value = false }
}
function closeDialog() {
  if (busy.value) return
  dialog.value?.close()
  modal.value = null
  formError.value = ''
  contactID.value = ''; contactName.value = ''; contactEmail.value = ''; memberships.value = []
  groupID.value = ''; groupName.value = ''
}
async function openDialog(mode: NonNullable<typeof modal.value>) {
  formError.value = ''
  modal.value = mode
  await nextTick()
  if (dialog.value && !dialog.value.open) dialog.value.showModal()
  if (mode.startsWith('delete')) dialog.value?.querySelector<InstanceType<typeof globalThis.HTMLButtonElement>>('[data-cancel]')?.focus()
}
function editContact(contact?: Contact, duplicate = false) {
  contactID.value = duplicate ? '' : contact?.id || ''
  contactName.value = contact ? contact.name + (duplicate ? ' (copy)' : '') : ''
  contactEmail.value = duplicate ? '' : contact?.email || ''
  memberships.value = [...(contact?.group_ids || (selectedGroup.value ? [selectedGroup.value] : []))]
  void openDialog('contact')
}
function editGroup(group?: ContactGroup) {
  groupID.value = group?.id || ''
  groupName.value = group?.name || ''
  void openDialog('group')
}
function askDeleteContact(contact: Contact) {
  contactID.value = contact.id
  contactName.value = contact.name
  contactEmail.value = contact.email
  void openDialog('delete-contact')
}
async function submit() {
  if (busy.value || !session.unlocked) return
  const active = current()
  busy.value = true
  formError.value = ''
  try {
    if (modal.value === 'contact') {
      const result = await saveContact({ id: contactID.value, name: contactName.value, email: contactEmail.value, group_ids: [...memberships.value] })
      if (active()) contacts.value = result
    } else if (modal.value === 'delete-contact') {
      const result = await deleteContact(contactID.value)
      if (active()) contacts.value = result
    } else if (modal.value === 'group' || modal.value === 'delete-group') {
      const deleting = modal.value === 'delete-group'
      const result = deleting ? await deleteContactGroup(groupID.value) : await saveContactGroup({ id: groupID.value, name: groupName.value })
      if (active()) {
        contacts.value = result.contacts
        groups.value = result.groups
        if (deleting && selectedGroup.value === groupID.value) selectedGroup.value = null
      }
    }
    if (active()) { busy.value = false; closeDialog() }
  } catch (value) { if (active()) formError.value = value instanceof Error ? value.message : 'Could not save your address book.' }
  finally { if (active()) busy.value = false }
}
function clear() {
  generation++
  busy.value = false
  closeDialog()
  contacts.value = []; groups.value = []; query.value = ''; selectedGroup.value = null; error.value = ''; loading.value = false
}
watch([() => session.userID, () => session.vaultEpoch, () => session.unlocked], () => {
  clear()
  if (session.unlocked) void load()
}, { immediate: true })
onBeforeUnmount(clear)
</script>

<template>
  <div class="app-shell">
    <AppHeader />
    <main v-if="session.loading" class="center-state">{{ t("Restoring session…") }}</main>
    <main v-else-if="!session.authenticated" class="center-state"><h1>{{ t("Session ended") }}</h1><RouterLink class="primary" to="/login">{{ t("Sign in") }}</RouterLink></main>
    <main v-else-if="!session.unlocked" class="center-state"><VaultUnlock /></main>
    <main v-else class="contacts-page">
      <div class="workspace-heading"><div><h1>{{ t("Contacts") }}</h1><p>{{ plural("{count} contact", "{count} contacts", contacts.length) }} · {{ formatNumber(groups.length) }} {{ groups.length === 1 ? t("group") : t("groups") }}</p></div><button type="button" class="primary" :disabled="loading || !!error" @click="editContact()"><AppIcon name="plus" :size="17" /><span>{{ t("New contact") }}</span></button></div>
      <p class="contacts-privacy quiet">{{ t("Your address book is encrypted in your vault. Only contacts you add are saved.") }}</p>
      <RecipientHistorySettings @changed="load" />
      <ExternalContacts @changed="load" />
      <GoogleContacts @changed="load" />
      <ContactTransfer :book="{contacts,groups}" :disabled="loading||!!error" @imported="load" />
      <p v-if="error" class="error" role="alert">{{ t(error) }} <button type="button" class="text-button" @click="load">{{ t("Try again") }}</button></p>
      <div class="contacts-layout" :aria-busy="loading">
        <aside class="contact-groups" :aria-label="t('Contact groups')">
          <header><h2>{{ t("Groups") }}</h2><button type="button" class="icon-button" :aria-label="t('New group')" :title="t('New group')" :disabled="loading || !!error" @click="editGroup()"><AppIcon name="plus" :size="17" /></button></header>
          <nav :aria-label="t('Filter contacts by group')">
            <div class="contact-group-row"><button type="button" class="contact-group-choice" :class="{ active: selectedGroup === null }" :aria-pressed="selectedGroup === null" @click="selectedGroup = null"><AppIcon name="contacts" :size="17" /><span>{{ t("All contacts") }}</span><b>{{ formatNumber(contacts.length) }}</b></button></div>
            <div class="contact-group-row"><button type="button" class="contact-group-choice" :class="{ active: selectedGroup === '' }" :aria-pressed="selectedGroup === ''" @click="selectedGroup = ''"><AppIcon name="folder" :size="17" /><span>{{ t("Ungrouped") }}</span><b>{{ formatNumber(ungrouped) }}</b></button></div>
            <div v-for="group in sortedGroups" :key="group.id" class="contact-group-row"><button type="button" class="contact-group-choice" :class="{ active: selectedGroup === group.id }" :aria-pressed="selectedGroup === group.id" @click="selectedGroup = group.id"><AppIcon name="folder" :size="17" /><span>{{ group.name }}</span><b>{{ groupCounts.get(group.id) || 0 }}</b></button><button type="button" class="contact-group-edit" :aria-label="t('Edit group {0}', {'0': group.name})" :title="t('Edit group {0}', {'0': group.name})" @click="editGroup(group)"><AppIcon name="edit" :size="15" /></button></div>
          </nav>
        </aside>
        <section class="contact-directory" :aria-label="t('Address book')">
          <header><h2>{{ selectionName }} <small>{{ formatNumber(matching.length) }}</small></h2><label class="contacts-search"><AppIcon name="search" :size="17" /><span class="sr-only">{{ t("Search contacts") }}</span><input v-model="query" type="search" maxlength="254" :placeholder="t('Search name or email')"></label></header>
          <p v-if="loading" class="contacts-empty" role="status">{{ t("Loading contacts…") }}</p>
          <ul v-else-if="matching.length" class="contact-list" :aria-label="t('Saved contacts')">
            <li v-for="contact in matching" :key="contact.id">
              <span class="contact-avatar" aria-hidden="true">{{ contact.name.charAt(0).toLocaleUpperCase() }}</span>
              <div class="contact-details"><strong>{{ contact.name }}</strong><span><bdi dir="ltr">{{ contact.email }}</bdi></span><div v-if="contact.group_ids?.length" class="contact-memberships"><span v-for="id in contact.group_ids.filter(id => groupNames.has(id))" :key="id">{{ groupNames.get(id) }}</span></div></div>
              <div class="contact-row-actions"><button type="button" class="icon-button" :aria-label="t('Edit contact {0}', {'0': contact.name})" :title="t('Edit')" @click="editContact(contact)"><AppIcon name="edit" :size="16" /></button><button type="button" class="icon-button" :aria-label="t('Duplicate contact {0}', {'0': contact.name})" :title="t('Duplicate')" @click="editContact(contact, true)"><AppIcon name="copy" :size="16" /></button><button type="button" class="icon-button" :aria-label="t('Delete contact {0}', {'0': contact.name})" :title="t('Delete')" @click="askDeleteContact(contact)"><AppIcon name="trash" :size="16" /></button></div>
            </li>
          </ul>
          <div v-else-if="!error" class="contacts-empty" role="status"><AppIcon :name="query ? 'search' : 'contacts'" :size="30" /><h3>{{ query ? t("No matching contacts") : t("No contacts yet") }}</h3><p>{{ query ? t("Try another name or email address.") : selectedGroup === null ? t("Add your first contact to get started.") : t("Add a contact or edit an existing one to organize this group.") }}</p></div>
        </section>
      </div>
    </main>
    <Teleport to="body">
      <dialog v-if="modal" ref="dialog" class="contacts-dialog" :aria-labelledby="titleID" @cancel.prevent="closeDialog" @click="($event.target === dialog) && closeDialog()">
        <section @click.stop>
          <header><h2 :id="titleID">{{ t(modalTitle) }}</h2><button type="button" class="icon-button" :aria-label="t('Close contact dialog')" :disabled="busy" @click="closeDialog"><AppIcon name="close" :size="18" /></button></header>
          <form @submit.prevent="submit"><fieldset :disabled="busy">
            <template v-if="modal === 'contact'">
              <label>{{ t("Contact name") }}<input v-model="contactName" autofocus required maxlength="128" autocomplete="off"></label>
              <label>{{ t("Email address") }}<input v-model="contactEmail" type="email" required maxlength="254" autocomplete="off"></label>
              <fieldset v-if="groups.length" class="contact-group-options"><legend>{{ t("Groups") }}</legend><label v-for="group in sortedGroups" :key="group.id" class="contact-membership-option"><input v-model="memberships" type="checkbox" :value="group.id">{{ group.name }}</label></fieldset>
            </template>
            <label v-else-if="modal === 'group'">{{ t("Group name") }}<input v-model="groupName" autofocus required maxlength="128" autocomplete="off"></label>
            <p v-else-if="modal === 'delete-contact'">{{ t("Remove") }} <strong>{{ contactName }}</strong> ({{ contactEmail }}{{ t(") from your contacts?") }}</p>
            <p v-else>{{ t("Delete the group") }} <strong>{{ groupName }}</strong>{{ t("? Its contacts will stay in your address book.") }}</p>
            <p v-if="formError" class="error" role="alert">{{ t(formError) }}</p>
            <div class="contacts-dialog-actions"><button v-if="modal === 'group' && groupID" type="button" class="text-button contact-delete-group" @click="openDialog('delete-group')">{{ t("Delete group") }}</button><button data-cancel type="button" class="secondary" @click="closeDialog">{{ t("Cancel") }}</button><button type="submit" :class="modal.startsWith('delete') ? 'danger-button' : 'primary'">{{ busy ? t("Saving…") : modal.startsWith('delete') ? t("Delete") : t("Save") }}</button></div>
          </fieldset></form>
        </section>
      </dialog>
    </Teleport>
  </div>
</template>

<style scoped>
.contacts-page { max-width: 1260px; margin: 0 auto; }
.contacts-privacy { margin: -4px 0 20px; }
.contacts-layout { display: grid; grid-template-columns: 230px minmax(0, 1fr); align-items: start; gap: 18px; min-width: 0; }
.contact-groups, .contact-directory { min-width: 0; background: var(--panel); border: 1px solid var(--line); border-radius: 14px; }
.contact-groups { padding: 14px 10px; }
.contact-groups header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 0 5px 12px; }
.contact-groups h2 { margin: 0; font-size: .88rem; }
.contact-groups nav { display: grid; gap: 4px; }
.contact-group-row { display: flex; align-items: center; min-width: 0; gap: 3px; }
.contact-group-choice { display: flex; align-items: center; gap: 9px; flex: 1; min-width: 0; min-height: 42px; padding: 9px; color: var(--ink); border: 1px solid transparent; border-radius: 9px; background: transparent; text-align: start; }
.contact-group-choice span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: .8rem; }
.contact-group-choice b { font-size: .7rem; font-weight: 400; color: var(--muted); }
.contact-group-choice:hover, .contact-group-choice.active { background: var(--selected); border-color: var(--line); }
.contact-group-edit { display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 36px; flex-shrink: 0; padding: 0; border: 0; border-radius: 8px; color: var(--muted); background: transparent; }
.contact-group-edit:hover { background: var(--raised); color: var(--ink); }
.contact-directory > header { display: flex; align-items: center; flex-wrap: wrap; gap: 14px; padding: 18px; border-bottom: 1px solid var(--line); }
.contact-directory h2 { margin: 0; font-size: 1rem; }
.contact-directory h2 small { margin-inline-start: 5px; color: var(--muted); font-size: .75rem; font-weight: 400; }
.contacts-search { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 150px; max-width: 300px; margin-inline-start: auto; padding: 0 11px; border: 1px solid var(--line); border-radius: 9px; color: var(--muted); background: var(--input); }
.contacts-search input { min-height: 40px; padding: 9px 0; border: 0; box-shadow: none; background: transparent; font-size: .8rem; }
.contact-list { list-style: none; padding: 0; margin: 0; }
.contact-list > li { display: grid; grid-template-columns: 40px minmax(0, 1fr) auto; align-items: center; gap: 14px; padding: 18px; border-bottom: 1px solid var(--line); }
.contact-list > li:last-child { border-bottom: 0; }
.contact-avatar { width: 40px; height: 40px; display: grid; place-items: center; border: 1px solid var(--line); border-radius: 12px; color: var(--accent); background: var(--selected); font-weight: 600; }
.contact-details { display: grid; gap: 3px; min-width: 0; overflow-wrap: anywhere; }
.contact-details > strong { font-size: .9rem; font-weight: 550; }
.contact-details > span { color: var(--muted); font-size: .8rem; }
.contact-memberships { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 4px; }
.contact-memberships span { padding: 2px 7px; border-radius: 5px; background: var(--raised); color: var(--muted); font-size: .67rem; }
.contact-row-actions { display: flex; gap: 5px; }
.contacts-empty { display: grid; justify-items: center; gap: 10px; padding: 48px 20px; text-align: center; color: var(--muted); }
.contacts-empty h3, .contacts-empty p { margin: 0; }
.contacts-empty h3 { font-size: 1rem; }
.contacts-empty p { max-width: 350px; font-size: .82rem; }
.contacts-dialog { width: min(480px, calc(100% - 28px)); max-height: calc(100dvh - 28px); padding: 0; overflow: auto; color: var(--ink); background: var(--panel); border: 1px solid var(--line); border-radius: 16px; box-shadow: 0 20px 80px #0005; }
.contacts-dialog::backdrop { background: #030712a6; backdrop-filter: blur(4px); }
.contacts-dialog header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 20px; border-bottom: 1px solid var(--line); }
.contacts-dialog h2 { margin: 0; font-size: 1.1rem; }
.contacts-dialog form { padding: 20px; }
.contacts-dialog fieldset { display: grid; gap: 16px; }
.contacts-dialog p { margin: 0; font-size: .85rem; overflow-wrap: anywhere; }
.contacts-dialog .contact-group-options { max-height: 200px; overflow: auto; gap: 7px; padding: 8px 0; }
.contact-group-options legend { font-size: .82rem; font-weight: 550; }
.contact-membership-option { display: flex; align-items: center; gap: 9px; min-height: 32px; overflow-wrap: anywhere; }
.contacts-dialog-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 10px; }
.contact-delete-group { margin-inline-end: auto; color: var(--error); }
@media (max-width: 680px) {
  .contacts-layout { grid-template-columns: minmax(0, 1fr); gap: 14px; }
  .contacts-page .workspace-heading h1 { font-size: 1.55rem; }
  .contacts-privacy { font-size: .73rem; }
  .contact-groups { padding: 10px; }
  .contact-groups header { margin-bottom: 6px; }
  .contact-groups nav { display: flex; gap: 6px; overflow-x: auto; padding-bottom: 4px; }
  .contact-group-row { flex: 0 0 auto; }
  .contact-group-choice { min-height: 44px; }
  .contact-group-edit { width: 38px; height: 44px; }
  .contact-directory > header { padding: 14px; gap: 12px; }
  .contacts-search { flex-basis: 100%; max-width: none; }
  .contact-list > li { grid-template-columns: 34px minmax(0, 1fr); gap: 10px; padding: 14px; }
  .contact-avatar { width: 34px; height: 34px; align-self: start; border-radius: 10px; }
  .contact-row-actions { grid-column: 2; }
  .contact-row-actions .icon-button { width: 40px; height: 40px; }
  .contacts-dialog-actions .contact-delete-group { flex-basis: 100%; text-align: start; }
}
</style>
