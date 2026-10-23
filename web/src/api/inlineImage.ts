const pattern=/^[a-zA-Z0-9_.@-]{1,128}$/
export const validCID=(value:string)=>pattern.test(value)
export const inlineFiles=new WeakMap<File,string>()
export function markInline(file:File,id:string){if(!validCID(id)||!['image/png','image/jpeg'].includes(file.type)||file.size>2*1024*1024)throw new Error('Inline images must be PNG or JPEG, at most 2 MiB.');inlineFiles.set(file,id)}
