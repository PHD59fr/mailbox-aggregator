import {readCollection} from './vaultCollection'
import {APIError} from './client'
import type {MessageFilters} from './reading'
import type {MailSource} from './mailSources'

export interface Contact {id:string;name:string;email:string;group_ids?:string[]}
export interface ContactGroup {id:string;name:string}
export interface SavedSearch {alert?:boolean;tagFilter?:string;id:string;name:string;filters:MessageFilters;accountID:string;folder:string;view?:string;scope?:'current'|'all';sources?:MailSource[]}
interface AddressBook {id:'address-book';contacts:Contact[];groups?:ContactGroup[]}
export interface ContactBook {contacts:Contact[];groups:ContactGroup[]}
export interface ContactImport {name:string;email:string;groups:string[]}
interface Searches {id:'saved-searches';searches:SavedSearch[]}

// Retry a precise mutation on freshly decrypted preferences, preserving edits
// to unrelated settings and records from other tabs.
async function mutate<T extends {id:string}>(id:string,empty:()=>T,change:(record:T)=>void){
 for(let attempt=0;attempt<3;attempt++){
  const collection=await readCollection<T>('preferences')
  const record=collection.records[id]||empty()
  change(record);collection.records[id]=record
  try{await collection.save();collection.assertUnlocked();return record}
  catch(error){if(attempt<2&&error instanceof APIError&&error.status===409&&error.code==='REVISION_CONFLICT')continue;throw error}
 }
 throw new Error('Preferences changed. Reload and try again.')
}
export async function readPreference<T extends {id:string}>(id:string,empty:()=>T){return (await readCollection<T>('preferences')).records[id]||empty()}
export const updatePreference = mutate
export async function listContacts(){
 const collection=await readCollection<AddressBook>('preferences')
 return collection.records['address-book']?.contacts||[]
}
export async function listContactBook():Promise<ContactBook>{
 const collection=await readCollection<AddressBook>('preferences')
 const book=collection.records['address-book']
 return {contacts:book?.contacts||[],groups:book?.groups||[]}
}
export async function saveContact(contact:Contact){
 const base={id:contact.id||crypto.randomUUID(),name:contact.name.trim(),email:contact.email.trim()}
 if(!base.name||base.name.length>128||base.email.length>254||!/^[^\s<>@]+@[^\s<>@]+$/.test(base.email))throw new Error('Enter a name and valid email address.')
 return (await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[]}),record=>{
  const memberships=contact.group_ids??record.contacts.find(item=>item.id===base.id)?.group_ids??[]
  const groups=new Set((record.groups||[]).map(group=>group.id))
  if(!Array.isArray(memberships)||memberships.length>100||memberships.some(id=>typeof id!=='string'||!groups.has(id)))throw new Error('A selected group no longer exists. Reload your contacts.')
  const groupIDs=[...new Set(memberships)]
  const value={...base,...(groupIDs.length?{group_ids:groupIDs}:{})}
  if(record.contacts.some(item=>item.id!==value.id&&item.email.toLowerCase()===value.email.toLowerCase()))throw new Error('This contact address already exists.')
  if(!record.contacts.some(item=>item.id===value.id)&&record.contacts.length>=1000)throw new Error('Maximum 1000 contacts reached.')
  record.contacts=[...record.contacts.filter(item=>item.id!==value.id),value]
 })).contacts
}
export async function deleteContact(id:string){return (await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[]}),record=>{record.contacts=record.contacts.filter(item=>item.id!==id)})).contacts}
export async function importContacts(entries:ContactImport[]){
 let added=0,duplicates=0
 const book=await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[],groups:[]}),record=>{
  added=0;duplicates=0
  const contacts=record.contacts.map(contact=>({...contact,group_ids:[...(contact.group_ids||[])]}))
  const groups=[...(record.groups||[])]
  const byEmail=new Map(contacts.map(contact=>[contact.email.toLowerCase(),contact]))
  const byGroup=new Map(groups.map(group=>[group.name.toLowerCase(),group.id]))
  for(const entry of entries){
   const name=entry.name.trim(),email=entry.email.trim()
   if(!name||name.length>128||email.length>254||!/^[^\s<>@]+@[^\s<>@]+$/.test(email)||!Array.isArray(entry.groups)||entry.groups.some(group=>typeof group!=='string'||!group.trim()||group.trim().length>128))throw new Error('Invalid contact in import. No contacts were saved.')
   let contact=byEmail.get(email.toLowerCase())
   if(contact)duplicates++
   else{contact={id:crypto.randomUUID(),name,email,group_ids:[]};contacts.push(contact);byEmail.set(email.toLowerCase(),contact);added++}
   for(const label of entry.groups){
    const trimmed=label.trim(),key=trimmed.toLowerCase()
    let id=byGroup.get(key)
    if(!id){id=crypto.randomUUID();groups.push({id,name:trimmed});byGroup.set(key,id)}
    if(!contact.group_ids.includes(id))contact.group_ids.push(id)
   }
  }
  if(contacts.length>1000)throw new Error('This import would exceed 1000 contacts. No contacts were saved.')
  if(groups.length>100)throw new Error('This import would exceed 100 groups. No contacts were saved.')
  record.contacts=contacts;record.groups=groups
 })
 return {contacts:book.contacts,groups:book.groups||[],added,duplicates}
}
export async function saveContactGroup(group:ContactGroup):Promise<ContactBook>{
 const value={id:group.id||crypto.randomUUID(),name:group.name.trim()}
 if(!value.name||value.name.length>128)throw new Error('Choose a group name (maximum 128 characters).')
 const book=await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[],groups:[]}),record=>{
  const groups=record.groups||[]
  if(groups.some(item=>item.id!==value.id&&item.name.toLowerCase()===value.name.toLowerCase()))throw new Error('A group with this name already exists.')
  if(!groups.some(item=>item.id===value.id)&&groups.length>=100)throw new Error('Maximum 100 groups reached.')
  record.groups=[...groups.filter(item=>item.id!==value.id),value]
 })
 return {contacts:book.contacts,groups:book.groups||[]}
}
export async function deleteContactGroup(id:string):Promise<ContactBook>{
 const book=await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[],groups:[]}),record=>{
  record.groups=(record.groups||[]).filter(item=>item.id!==id)
  record.contacts=record.contacts.map(contact=>({...contact,group_ids:(contact.group_ids||[]).filter(group=>group!==id)}))
 })
 return {contacts:book.contacts,groups:book.groups||[]}
}
export async function listSavedSearches(){return (await readCollection<Searches>('preferences')).records['saved-searches']?.searches||[]}
export async function saveSearch(search:SavedSearch){
 const value={...search,id:search.id||crypto.randomUUID(),name:search.name.trim(),filters:{...search.filters}}
 if(!value.name||value.name.length>128)throw new Error('Choose a search name (maximum 128 characters).')
 return (await mutate<Searches>('saved-searches',()=>({id:'saved-searches',searches:[]}),record=>{
  if(!record.searches.some(item=>item.id===value.id)&&record.searches.length>=100)throw new Error('Maximum 100 saved searches reached.')
  record.searches=[...record.searches.filter(item=>item.id!==value.id),value]
  if(record.searches.filter(s=>s.alert).length>3)throw new Error('Enable at most three saved-search alerts.')
 })).searches
}
export async function deleteSearch(id:string){return (await mutate<Searches>('saved-searches',()=>({id:'saved-searches',searches:[]}),record=>{record.searches=record.searches.filter(item=>item.id!==id)})).searches}
