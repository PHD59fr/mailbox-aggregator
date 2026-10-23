<script setup lang="ts">
import {computed} from 'vue'
const props=defineProps<{text:string;terms:string[]}>()
// Text interpolation only; search text and message subjects never become HTML.
const chunks=computed(()=>{
 const terms=[...new Set(props.terms.map(s=>s.trim()).filter(s=>s&&s.length<=512))].slice(0,10).sort((a,b)=>b.length-a.length)
 if(!terms.length)return [{text:props.text,match:false}]
 const pattern=new RegExp(terms.map(t=>t.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')).join('|'),'gi')
 const parts:{text:string;match:boolean}[]=[];let at=0
 for(const match of props.text.matchAll(pattern)){const index=match.index!;if(index>at)parts.push({text:props.text.slice(at,index),match:false});parts.push({text:match[0],match:true});at=index+match[0].length}
 if(at<props.text.length)parts.push({text:props.text.slice(at),match:false});return parts
})
</script>
<template><template v-for="(chunk,index) in chunks" :key="index"><mark v-if="chunk.match">{{chunk.text}}</mark><template v-else>{{chunk.text}}</template></template></template>
<style scoped>mark{background:var(--selected);color:var(--ink);text-decoration:underline;text-decoration-thickness:2px}</style>
