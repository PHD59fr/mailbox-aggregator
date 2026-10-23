import {readPreference,updatePreference,type SavedSearch} from './preferences'
export interface SearchHistory{id:'search-history';remember:boolean;searches:(SavedSearch&{savedAt:number})[]}
export const emptySearchHistory=():SearchHistory=>({id:'search-history',remember:false,searches:[]})
export function cleanSearchHistory(record:SearchHistory){return {...record,searches:(record.searches||[]).filter(s=>s.savedAt>Date.now()-30*86400000).sort((a,b)=>b.savedAt-a.savedAt).slice(0,20)}}
export async function readSearchHistory(){return cleanSearchHistory(await readPreference<SearchHistory>('search-history',emptySearchHistory))}
export async function rememberSearch(search:SavedSearch){
 if(!Object.values(search.filters).some(Boolean))return
 await updatePreference<SearchHistory>('search-history',emptySearchHistory,record=>{
  if(!record.remember)return
  const signature=(s:SavedSearch)=>JSON.stringify([s.accountID,s.folder,s.view,s.scope,s.sources,s.filters,s.tagFilter])
  record.searches=cleanSearchHistory({...record,searches:[{...search,id:crypto.randomUUID(),alert:false,savedAt:Date.now()},...record.searches.filter(s=>signature(s)!==signature(search))]}).searches
 })
}
