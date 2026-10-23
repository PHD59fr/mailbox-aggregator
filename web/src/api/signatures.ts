// Signatures are plain text preferences. Escape them before including HTML;
// they cannot inject headers or an executable HTML fragment into the composer.
export function withSignature(text:string,html:string,signature:string){
 if(!signature)return {text,html}
 const escaped=signature.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/\r?\n/g,'<br>')
 return {text:`${text}\n\n-- \n${signature}`,html:html?`${html}<p>-- <br>${escaped}</p>`:''}
}
