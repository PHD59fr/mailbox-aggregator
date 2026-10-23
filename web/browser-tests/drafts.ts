import { ComposeDraftSession, draftAttachmentFile, migrateComposeDrafts, type DraftInput } from '../src/crypto/composeDraft'
import { getStoredDraft, replaceStoredDraft } from '../src/crypto/draftStorage'
import { derivePasswordKey, cancelKeyDerivation } from '../src/crypto/argon2'
import { argon2idAsync } from '@noble/hashes/argon2.js'

const result = globalThis.document.getElementById('result')!
async function run() {
  globalThis.sessionStorage.clear()
  const vmk = crypto.getRandomValues(new Uint8Array(32))
  const input: DraftInput = { action: 'reply', original: null, inReplyTo:'<parent@example.test>', references:['<root@example.test>','<parent@example.test>'], to: 'writer@example.test', cc: '', bcc: '', subject: 'Browser-only secret subject', text: 'Private body', html: '', showCc: false, showBcc: false, selectedSentID: '' }
  let unlocked = true
  const session = new ComposeDraftSession(vmk, 'browser-test', () => unlocked, () => input)
  const attachment = new File(['confidential attachment payload'], 'Private-dismissal-Alice.pdf', { type: 'application/pdf' })
  await session.addAttachment(attachment)
  await session.flush()
  const record = (await getStoredDraft(session.id))!
  const serialized = JSON.stringify(record)
  for (const plaintext of [input.subject, input.text, attachment.name, 'confidential attachment payload']) {
    if (serialized.includes(plaintext)) throw new Error('Plaintext found in IndexedDB')
  }
  unlocked = false
  session.dispose()
  const locked = new ComposeDraftSession(vmk, 'browser-test', () => false, () => input)
  if (await locked.load() !== null) throw new Error('Locked draft was readable')
  const restored = new ComposeDraftSession(vmk, 'browser-test', () => true, () => input)
  const loaded = await restored.load()
  if (JSON.stringify(loaded?.meta.references)!==JSON.stringify(input.references)) throw new Error('Reply References were not restored')
  if (loaded?.meta.subject !== input.subject || loaded.attachments[0]?.name !== attachment.name) throw new Error('Draft fields were not restored')
  if (await draftAttachmentFile(loaded.attachments[0]).text() !== await attachment.text()) throw new Error('Attachment was not restored')
  // Simulate a newer autosave while an older background migration is pending.
  input.subject = 'Newest unsent browser version'
  await restored.save()
  await replaceStoredDraft(record, { ...record, savedAt: record.savedAt + 100 })
  if ((await restored.load())?.meta.subject !== input.subject) throw new Error('Migration overwrote a newer draft')
  restored.dispose()
  const nextKey=crypto.getRandomValues(new Uint8Array(32))
  await migrateComposeDrafts(nextKey,'browser-test',()=>true,[vmk])
  const rotated=new ComposeDraftSession(nextKey,'browser-test',()=>true,()=>input)
  const migrated=await rotated.load()
  if(migrated?.meta.subject!==input.subject || await draftAttachmentFile(migrated.attachments[0]!).text()!==await attachment.text())throw new Error('Key rotation lost browser draft content')
  await rotated.clear()

  const salt=new Uint8Array(16).fill(7)
  let heartbeats=0
  const ticker=setInterval(()=>heartbeats++,10)
  let key:Uint8Array
  try{key=await derivePasswordKey('Worker password \u212b',salt)}finally{clearInterval(ticker)}
  if(heartbeats<3)throw new Error('Argon2 blocked the UI thread')
  const expected=await argon2idAsync(new TextEncoder().encode('Worker password Å'),salt,{m:65536,t:3,p:1,dkLen:32,asyncTick:10})
  if(!key!.every((value,index)=>value===expected[index]))throw new Error('Worker Argon2 differs from the pinned reference')
  const cancelled=derivePasswordKey('cancel this work',salt).then(()=>false,()=>true)
  cancelKeyDerivation()
  if(!await cancelled)throw new Error('Lock did not cancel Argon2')
  const restarted=await derivePasswordKey('Worker password Å',salt)
  if(!restarted.every((value,index)=>value===expected[index]))throw new Error('Worker did not recover after lock')
  key!.fill(0);expected.fill(0);restarted.fill(0);cancelKeyDerivation()
}
run().then(() => { result.textContent = 'PASS'; result.dataset.status = 'passed' }, error => {
  result.textContent = String(error); result.dataset.status = 'failed'
})
