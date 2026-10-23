import {locale,t} from '../i18n'

// Keep previews local and inert, including when a quoted email is in the draft.
// Formatting classes avoid relaxing the application's ban on inline CSS.
export function composePreviewDocument(value:string,images:Record<string,string>={}):string {
  const document=new DOMParser().parseFromString(value,'text/html')
  const tags=new Set(['P','BR','STRONG','B','EM','I','U','S','DEL','UL','OL','LI','BLOCKQUOTE','A','CODE','SPAN','H1','H2','H3','H4','H5','H6','DIV','PRE','HR','TABLE','THEAD','TBODY','TFOOT','TR','TD','TH'])
  const colors=['59, 130, 246','168, 85, 247','225, 29, 72','217, 119, 6','16, 185, 129','100, 116, 139']
  const highlights=['191, 219, 254','233, 213, 255','254, 205, 211','254, 240, 138','187, 247, 208','226, 232, 240']
  for(const element of document.body.querySelectorAll<HTMLElement>('*')) {
    if(element.tagName==='IMG'){
      const src=element.getAttribute('src')||'',id=src.startsWith('cid:')?src.slice(4):'',source=images[id]
      if(!source||!/^data:image\/(png|jpeg);base64,[A-Za-z0-9+/=]+$/.test(source)){element.remove();continue}
      const alt=element.getAttribute('alt')||'Inline image';for(const attr of Array.from(element.attributes))element.removeAttribute(attr.name);element.setAttribute('src',source);element.setAttribute('alt',alt);continue
    }
    if(!tags.has(element.tagName)){element.remove();continue}
    const classes:string[]=[]
    const color=colors.indexOf(element.style.color.replace(/^rgb\(|\)$/g,''))
    const highlight=highlights.indexOf(element.style.backgroundColor.replace(/^rgb\(|\)$/g,''))
    if(color>=0)classes.push(`email-color-${color}`)
    if(highlight>=0)classes.push(`email-highlight-${highlight}`)
    if(['12px','14px','16px','18px','20px','24px'].includes(element.style.fontSize))classes.push(`email-size-${element.style.fontSize.slice(0,-2)}`)
    if(['left','center','right','justify'].includes(element.style.textAlign))classes.push(`email-align-${element.style.textAlign}`)
    const spans=['colspan','rowspan','start'].flatMap(name=>{
      const value=Number(element.getAttribute(name));return value>0 && value<=100 ? [[name,String(value)]] : []
    })
    for(const attribute of Array.from(element.attributes))element.removeAttribute(attribute.name)
    if(classes.length)element.className=classes.join(' ')
    for(const [name,value] of spans)element.setAttribute(name!,value!)
  }
  const origin=globalThis.location.origin
  return `<!doctype html><html lang="${value ? '' : locale.value}" dir="auto"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${origin}; img-src data:; base-uri 'none'; form-action 'none'"><link rel="stylesheet" href="${origin}/compose-preview.css"></head><body>${document.body.innerHTML || '<p class="email-placeholder">'+t('Your formatted message will appear here.')+'</p>'}</body></html>`
}
