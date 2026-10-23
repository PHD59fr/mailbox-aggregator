<script setup lang="ts">
import AppIcon from './AppIcon.vue'
</script>

<template>
  <div class="auth-illustration" aria-hidden="true">
    <div class="floating-mail back"><div class="message-lines"><span></span><span></span><span></span></div></div>
    <div class="floating-mail"><span class="mail-symbol"><AppIcon name="mail" :size="27" /></span><div class="message-lines"><span></span><span></span><span></span></div><span class="mail-check"><AppIcon name="check" :size="17" /></span></div>
  </div>
</template>
