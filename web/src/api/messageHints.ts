
import { t } from '../i18n'
import type {Message} from './reading'
export function messageHints(message:Message){
 const hints:string[]=[],from=message.from[0],reply=message.reply_to?.[0]
 const domain=(email:string)=>email.toLowerCase().split('@')[1]||''
 if(reply&&from&&domain(reply.email)!==domain(from.email))hints.push(t("Replies go to {0}, whose domain differs from the sender. Verify unexpected payment or account requests independently.", {"0": reply.email}))
 if(from&&from.name){const shown=from.name.match(/[^\s<>@()[\]{}"']+@[^\s<>@()[\]{}"']+/g)||[];if(shown.some(email=>email.replace(/[),.;:]+$/,'').toLowerCase()!==from.email.toLowerCase()))hints.push('The sender display name contains a different email address. Check the actual From address.')}
 if(from&&(/(^|\.)xn--/i.test(domain(from.email))||Array.from(domain(from.email)).some(c=>c.charCodeAt(0)>126||c.charCodeAt(0)<32)))hints.push('The sender uses an internationalized domain. Check its spelling before trusting a sensitive request.')
 return hints
}
