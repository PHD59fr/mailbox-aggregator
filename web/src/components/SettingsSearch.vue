<script setup lang="ts">
import {t} from '../i18n'
import { computed, ref } from 'vue'
const query=ref('')
const sections=[
  {id:'appearance',name:'Appearance',keywords:'theme colors display'},
  {id:'appearance',name:'Interface language',keywords:'Interface direction'},
  {id:'appearance',name:'Display and keyboard',keywords:'Reading pane'},
  {id:'organization',name:'Tags, snooze and recent history',keywords:'Remember recently opened messages as encrypted references'},
  {id:'searches',name:'Saved searches and combined folders',keywords:'Search history (optional)'},
  {id:'accounts',name:'Receive email',keywords:'IMAP mailbox folder mapping password'},
  {id:'identities',name:'Send email',keywords:'SMTP sender signature reply-to bcc'},
  {id:'mail-clients',name:'Mail client access',keywords:'proxy gateway IMAPS SMTPS'},
  {id:'security',name:'Security',keywords:'lock inactivity images privacy'},
  {id:'sieve',name:'Server mail rules',keywords:'Sieve ManageSieve rules filter sender screen'},
  {id:'mfa',name:'Two-factor authentication',keywords:'TOTP MFA authenticator recovery codes'},
  {id:'oauth',name:'Connect Google or Microsoft',keywords:'OAuth Gmail Outlook token authorization'},
  {id:'credentials',name:'Password and vault key',keywords:'OPAQUE reauthenticate rotation'},
  {id:'import-export',name:'Import and export',keywords:'backup restore configuration'},
]
const matches=computed(()=>query.value.trim()?sections.filter(section=>(section.name+' '+section.keywords+' '+t(section.name)+' '+t(section.keywords)).toLowerCase().includes(query.value.trim().toLowerCase())):[])
function open(id:string){globalThis.document.getElementById(id)?.scrollIntoView({block:'start',behavior:'smooth'})}
</script>
<template><div class="settings-search"><label>{{ t("Find a setting") }}<input v-model="query" type="search" maxlength="128" :placeholder="t('For example: signature or folder')"></label><ul v-if="query.trim()" :aria-label="t('Matching settings')"><li v-for="section in matches" :key="section.name"><button type="button" class="text-button" @click="open(section.id)">{{t(section.name)}}</button></li><li v-if="!matches.length">{{ t("No matching section.") }}</li></ul></div></template>
