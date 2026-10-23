import { readdir, readFile, mkdtemp, rm } from 'node:fs/promises'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import assert from 'node:assert/strict'

const root=dirname(dirname(fileURLToPath(import.meta.url)))
const temporary=await mkdtemp(join(tmpdir(),'emailer-build-check-'))
async function output(directory){
  const files={}
  async function walk(relative){for(const entry of await readdir(join(directory,relative),{withFileTypes:true})){
    const name=join(relative,entry.name)
    if(entry.isDirectory())await walk(name)
    else{
      const bytes=await readFile(join(directory,name))
      if(name.endsWith('.js'))assert.ok(bytes.length<=500000,`${name} exceeds the 500 kB JavaScript chunk budget`)
      files[name]=createHash('sha256').update(bytes).digest('hex')
    }
  }}
  await walk('');return files
}
try{
  const first=await output(join(root,'web/dist'))
  execFileSync(process.execPath,[join(root,'web/node_modules/vite/bin/vite.js'),'build','--outDir',temporary,'--emptyOutDir'],{cwd:join(root,'web'),stdio:'pipe'})
  assert.deepEqual(await output(temporary),first,'Production assets are not reproducible')
  console.log(`Reproducible production build (${Object.keys(first).length} files), all JavaScript chunks <=500 kB`)
}finally{await rm(temporary,{recursive:true,force:true})}
