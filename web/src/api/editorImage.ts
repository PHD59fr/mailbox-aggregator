
import { t } from '../i18n'
import {Node} from '@tiptap/core'
import {validCID} from './inlineImage'
export function inlineNode(resolve:(id:string)=>string|undefined){return Node.create({
 name:'inlineImage',group:'block',atom:true,draggable:true,
 addAttributes(){return {src:{default:''},alt:{default:'Inline image'}}},
 parseHTML(){return [{tag:'img[src^="cid:"]',getAttrs:(element)=>validCID((element as HTMLElement).getAttribute('src')!.slice(4))?null:false}]},
 renderHTML({node}){return ['img',{src:node.attrs.src,alt:node.attrs.alt}]},
 renderText({node}){return `[${node.attrs.alt||'Inline image'}]`},
 addNodeView(){return ({node})=>{const dom=document.createElement('figure'),source=resolve(String(node.attrs.src).slice(4));if(source){const img=document.createElement('img');img.src=source;img.alt=node.attrs.alt;dom.append(img)}else{dom.textContent=t("[Inline image: {0}]", {"0": node.attrs.alt||'image'})};return {dom}}},
})}
