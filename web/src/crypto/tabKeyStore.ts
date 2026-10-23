interface StoredKey { key: CryptoKey; expiresAt: number }
async function database():Promise<IDBDatabase> {
  if(typeof indexedDB==='undefined') throw new Error('Encrypted tab storage is unavailable.')
  return new Promise((resolve,reject)=>{
    let blocked=false
    const request=indexedDB.open('mailbox-tab-resume-v1',1)
    request.onupgradeneeded=()=>{request.result.createObjectStore('keys')}
    request.onerror=()=>reject(request.error)
    request.onblocked=()=>{blocked=true;reject(new Error('Encrypted tab storage is blocked.'))}
    request.onsuccess=()=>{if(blocked)request.result.close();else resolve(request.result)}
  })
}
async function run<T>(mode:IDBTransactionMode,operation:(store:IDBObjectStore)=>IDBRequest<T>):Promise<T> {
  const db=await database()
  try {
    return await new Promise<T>((resolve,reject)=>{
      const transaction=db.transaction('keys',mode)
      const request=operation(transaction.objectStore('keys'))
      transaction.oncomplete=()=>resolve(request.result)
      transaction.onabort=transaction.onerror=()=>reject(transaction.error || new Error('Encrypted tab storage failed.'))
    })
  } finally {db.close()}
}
export async function putTabKey(id:string,key:CryptoKey) {
  await run('readwrite',store=>store.put({key,expiresAt:Date.now()+24*60*60*1000} satisfies StoredKey,id))
}
export async function getTabKey(id:string):Promise<CryptoKey|undefined> {
  const record=await run<StoredKey|undefined>('readonly',store=>store.get(id))
  if(!record || record.expiresAt<=Date.now()) {if(record) await deleteTabKey(id);return}
  return record.key
}
export async function deleteTabKey(id:string) {await run('readwrite',store=>store.delete(id))}
export async function pruneTabKeys() {
  const db=await database()
  try {
    await new Promise<void>((resolve,reject)=>{
      const transaction=db.transaction('keys','readwrite')
      const cursor=transaction.objectStore('keys').openCursor()
      cursor.onsuccess=()=>{const row=cursor.result;if(!row)return;if((row.value as StoredKey).expiresAt<=Date.now())row.delete();row.continue()}
      transaction.oncomplete=()=>resolve();transaction.onerror=transaction.onabort=()=>reject(transaction.error)
    })
  } finally {db.close()}
}
