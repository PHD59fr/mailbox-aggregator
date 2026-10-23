<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {listContacts,saveContact,deleteContact,type Contact} from '../api/preferences'
const contacts=ref<Contact[]>([]);const query=ref('');const name=ref('');const email=ref('');const id=ref('');const busy=ref(false);const error=ref('')
const matching=computed(()=>contacts.value.filter(item=>`${item.name} ${item.email}`.toLowerCase().includes(query.value.toLowerCase())).sort((a,b)=>a.name.localeCompare(b.name)))
onMounted(async()=>{try{contacts.value=await listContacts()}catch(value){error.value=value instanceof Error?value.message:'Could not load contacts.'}})
async function save(){if(busy.value)return;busy.value=true;error.value='';try{contacts.value=await saveContact({id:id.value,name:name.value,email:email.value});id.value='';name.value='';email.value=''}catch(value){error.value=value instanceof Error?value.message:'Could not save contact.'}finally{busy.value=false}}
async function remove(target:string){if(busy.value)return;busy.value=true;error.value='';try{contacts.value=await deleteContact(target);if(id.value===target){id.value='';name.value='';email.value=''}}catch(value){error.value=value instanceof Error?value.message:'Could not delete contact.'}finally{busy.value=false}}
</script>
<template>
 <section id="contacts"><h2>Contacts</h2><p>Only contacts you explicitly add are saved, encrypted in your vault. Recipient history is never collected automatically.</p>
  <label>Search contacts<input v-model="query" type="search"></label>
  <ul aria-label="Saved contacts"><li v-for="item in matching" :key="item.id"><span>{{item.name}} &lt;{{item.email}}&gt;</span><button type="button" :disabled="busy" @click="id=item.id;name=item.name;email=item.email">Edit</button><button type="button" :disabled="busy" @click="id='';name=`${item.name} (copy)`;email=''">Duplicate</button><button type="button" :disabled="busy" @click="remove(item.id)">Delete</button></li></ul>
  <form @submit.prevent="save"><fieldset :disabled="busy"><label>Contact name<input v-model="name" required maxlength="128" autocomplete="off"></label><label>Email address<input v-model="email" type="email" required maxlength="254" autocomplete="off"></label><button>{{id?'Save contact':'Add contact'}}</button><button v-if="id" type="button" @click="id='';name='';email=''">Cancel editing</button></fieldset></form>
  <p v-if="error" class="error" role="alert">{{error}}</p>
 </section>
</template>
