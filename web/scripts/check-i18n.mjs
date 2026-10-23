import process from 'node:process'
import {readFile,readdir} from 'node:fs/promises'
import {dirname,join} from 'node:path'
import {fileURLToPath} from 'node:url'
import {parse} from 'vue/compiler-sfc'
import ts from 'typescript'

const root=dirname(dirname(fileURLToPath(import.meta.url)))
const en=JSON.parse(await readFile(join(root,'src/i18n/en.json'),'utf8'))
const fr=JSON.parse(await readFile(join(root,'src/i18n/fr.json'),'utf8'))
const errors=[]
const holes=value=>JSON.stringify((value.match(/\{\w+\}/g)||[]).sort())
for(const key of new Set([...Object.keys(en),...Object.keys(fr)])) {
  if(!Object.hasOwn(en,key)||!Object.hasOwn(fr,key))errors.push(`Missing catalog key: ${key}`)
  else if(!fr[key].trim()||holes(key)!==holes(fr[key]))errors.push(`Invalid French placeholders: ${key}`)
}
async function files(directory){const result=[];for(const item of await readdir(directory,{withFileTypes:true})){const path=join(directory,item.name);if(item.isDirectory())result.push(...await files(path));else if(/\.(vue|ts)$/.test(path)&&!path.endsWith('.test.ts'))result.push(path)}return result}
function scan(source,file){const ast=ts.createSourceFile(file,source,ts.ScriptTarget.Latest,true);function visit(node){
  if(ts.isCallExpression(node)&&ts.isIdentifier(node.expression)&&['t','plural'].includes(node.expression.text)) {
    for(const argument of node.arguments.slice(0,node.expression.text==='plural'?2:1))if((ts.isStringLiteral(argument)||ts.isNoSubstitutionTemplateLiteral(argument))&&!Object.hasOwn(en,argument.text))errors.push(`${file}: uncatalogued ${argument.text}`)
  }
  ts.forEachChild(node,visit)
}visit(ast)}
for(const file of await files(join(root,'src'))) {
  const source=await readFile(file,'utf8')
  if(file.endsWith('.vue')) {
    const {descriptor}=parse(source)
    if(descriptor.scriptSetup)scan(descriptor.scriptSetup.content,file)
    function template(node){
      if(node.type===1&&(['pre','code','script','style'].includes(node.tag)||node.tag==='option'&&node.props.some(p=>p.name==='lang')))return
      if(node.type===2&&/[A-Za-z]{2}/.test(node.content))errors.push(`${file}: unextracted text: ${node.content.trim()}`)
      if(node.type===5)scan('('+node.content.content+')',file)
      for(const prop of node.props||[]){
        if(prop.type===7&&prop.exp)scan('('+prop.exp.content+')',file)
        if(prop.type===6&&['title','aria-label','alt','placeholder'].includes(prop.name)&&prop.value&&/[A-Za-z]{2}/.test(prop.value.content))errors.push(`${file}: unextracted ${prop.name}: ${prop.value.content}`)
      }
      for(const child of node.children||[])template(child)
    }
    if(descriptor.template?.ast)template(descriptor.template.ast)
  } else scan(source,file)
}
if(errors.length){process.stderr.write(errors.join('\n')+'\n');process.exitCode=1}
else process.stdout.write(`English/French catalogs: ${Object.keys(en).length} messages, placeholders and static UI extraction checked\n`)
