<script setup lang="ts">
import type {Address} from '../api/reading'
import AddMessageContact from './AddMessageContact.vue'
defineProps<{addresses:Address[];fallback?:string}>()
</script>
<template>
  <span class="message-addresses">
    <span v-for="(address,index) in addresses" :key="`${index}:${address.email}`" class="message-address">
      <bdi dir="auto"><template v-if="address.name">{{address.name}} &lt;</template><bdi dir="ltr">{{address.email}}</bdi><template v-if="address.name">&gt;</template></bdi><AddMessageContact :addresses="[address]" icon-only /><span v-if="index<addresses.length-1" aria-hidden="true">, </span>
    </span>
    <span v-if="!addresses.length">{{fallback}}</span>
  </span>
</template>
<style scoped>
.message-address { display:inline; }
.message-addresses { overflow-wrap:anywhere; }
</style>
