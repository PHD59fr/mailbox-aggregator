import {readCollection} from './vaultCollection'
import {APIError} from './client'
import type {MessageFilters} from './reading'

export interface Contact {id:string;name:string;email:string}
export interface SavedSearch {id:string;name:string;filters:MessageFilters;accountID:string;folder:string;view?:string}
interface AddressBook {id:'address-book';contacts:Contact[]}
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
export async function listContacts(){
 const collection=await readCollection<AddressBook>('preferences')
 return collection.records['address-book']?.contacts||[]
}
export async function saveContact(contact:Contact){
 const value={id:contact.id||crypto.randomUUID(),name:contact.name.trim(),email:contact.email.trim()}
 if(!value.name||value.name.length>128||value.email.length>254||!/^[^\s<>@]+@[^\s<>@]+$/.test(value.email))throw new Error('Enter a name and valid email address.')
 return (await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[]}),record=>{
  if(record.contacts.some(item=>item.id!==value.id&&item.email.toLowerCase()===value.email.toLowerCase()))throw new Error('This contact address already exists.')
  if(!record.contacts.some(item=>item.id===value.id)&&record.contacts.length>=1000)throw new Error('Maximum 1000 contacts reached.')
  record.contacts=[...record.contacts.filter(item=>item.id!==value.id),value]
 })).contacts
}
export async function deleteContact(id:string){return (await mutate<AddressBook>('address-book',()=>({id:'address-book',contacts:[]}),record=>{record.contacts=record.contacts.filter(item=>item.id!==id)})).contacts}
export async function listSavedSearches(){return (await readCollection<Searches>('preferences')).records['saved-searches']?.searches||[]}
export async function saveSearch(search:SavedSearch){
 const value={...search,id:search.id||crypto.randomUUID(),name:search.name.trim(),filters:{...search.filters}}
 if(!value.name||value.name.length>128)throw new Error('Choose a search name (maximum 128 characters).')
 return (await mutate<Searches>('saved-searches',()=>({id:'saved-searches',searches:[]}),record=>{
  if(!record.searches.some(item=>item.id===value.id)&&record.searches.length>=100)throw new Error('Maximum 100 saved searches reached.')
  record.searches=[...record.searches.filter(item=>item.id!==value.id),value]
 })).searches
}
export async function deleteSearch(id:string){return (await mutate<Searches>('saved-searches',()=>({id:'saved-searches',searches:[]}),record=>{record.searches=record.searches.filter(item=>item.id!==id)})).searches}
