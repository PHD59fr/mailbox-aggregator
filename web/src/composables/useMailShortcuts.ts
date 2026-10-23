import { onUnmounted } from 'vue'
import { useMailboxStore } from '../stores/mailboxes'
import { useSessionStore } from '../stores/session'
import type { WorkspaceDisplay } from '../api/workspaceDisplay'
export function useMailShortcuts(actions:Partial<Record<keyof WorkspaceDisplay['shortcuts'],()=>void>>){
  const mailbox=useMailboxStore(),session=useSessionStore()
  function keydown(event:InstanceType<typeof globalThis.KeyboardEvent>){
    if(!session.unlocked||!mailbox.display.keyboard||event.defaultPrevented||event.repeat||event.ctrlKey||event.metaKey||event.altKey)return
    const target=event.target
    if(target instanceof globalThis.Element&&target.closest('input,textarea,select,[contenteditable="true"],[role="textbox"],dialog,[role="dialog"]'))return
    if(globalThis.document.querySelector('dialog[open],[role="dialog"][aria-modal="true"]'))return
    for(const [action,key] of Object.entries(mailbox.display.shortcuts)){
      if(key&&event.key===key){const run=actions[action as keyof WorkspaceDisplay['shortcuts']];if(run){event.preventDefault();run()}return}
    }
  }
  globalThis.document.addEventListener('keydown',keydown)
  onUnmounted(()=>globalThis.document.removeEventListener('keydown',keydown))
}
