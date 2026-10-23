
export const defaultShortcuts=()=>({keyboard:false,shortcuts:{next:'j',previous:'k',close:'Escape',read:'u',star:'f',reply:'r',compose:'c',search:'/'}})
export interface WorkspaceDisplay {
  id:'workspace-display'
  density:'comfortable'|'compact'|'spacious'
  layout:'split'|'below'
  windowDays?:number
  windowLimit?:number
  colors:Record<string,string>
  keyboard:boolean
  shortcuts:ReturnType<typeof defaultShortcuts>['shortcuts']
}
export function accountColor(value:string|undefined){return ['blue','purple','rose','amber','green'].includes(value||'')?`account-${value}`:''}
