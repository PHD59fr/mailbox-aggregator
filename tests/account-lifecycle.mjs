import { spawn, execFileSync } from 'node:child_process'
import { mkdtemp, cp, readdir, readFile, writeFile, chmod, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, dirname, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { createHash } from 'node:crypto'
import net from 'node:net'
import assert from 'node:assert/strict'
import { build } from '../web/node_modules/esbuild/lib/main.js'

const root=dirname(dirname(fileURLToPath(import.meta.url)))
const binary=resolve(process.env.TEST_SERVER_BINARY || join(root,'mailbox-aggregator'))
const temporary=await mkdtemp(join(tmpdir(),'emailer-account-lifecycle-'))
let server
const environment=data=>({...process.env,APP_ENV:'development',APP_DATA_DIR:data,OPAQUE_SERVER_SETUP:'',SESSION_COOKIE_SECURE:'false',IMAP_PROXY_ENABLED:'false',SMTP_PROXY_ENABLED:'false',REGISTRATION_DISABLED:'false',LOG_LEVEL:'error'})
async function stop(){
  if(!server || server.exitCode!==null || server.signalCode!==null)return
  const exited=new Promise(resolve=>server.once('exit',resolve));server.kill('SIGTERM');await exited
}
async function start(data){
  const reserve=net.createServer();await new Promise(resolve=>reserve.listen(0,'127.0.0.1',resolve))
  const port=reserve.address().port;await new Promise(resolve=>reserve.close(resolve))
  const base=`http://127.0.0.1:${port}`
  server=spawn(binary,[],{cwd:temporary,env:{...environment(data),APP_LISTEN_ADDR:`127.0.0.1:${port}`},stdio:['ignore','pipe','pipe']})
  let output='';server.stdout.on('data',chunk=>output+=chunk);server.stderr.on('data',chunk=>output+=chunk)
  for(let attempt=0;attempt<100;attempt++){
    if(server.exitCode!==null)throw new Error(`Isolated server failed: ${output}`)
    try{if((await fetch(`${base}/readyz`)).ok)return base}catch{/* Starting. */}
    await new Promise(resolve=>setTimeout(resolve,50))
  }
  throw new Error('Isolated server readiness timeout')
}
async function manifest(directory){
  const result={}
  async function walk(path){for(const entry of await readdir(join(directory,path),{withFileTypes:true})){
    const name=join(path,entry.name);const info=await stat(join(directory,name))
    result[name]={mode:info.mode,hash:entry.isFile()?createHash('sha256').update(await readFile(join(directory,name))).digest('hex'):null}
    if(entry.isDirectory())await walk(name)
  }}
  await walk('');return result
}
try{
  const bundle=join(temporary,'lifecycle.mjs')
  await build({entryPoints:[join(root,'tests/account-lifecycle.ts')],nodePaths:[join(root,'web/node_modules')],bundle:true,platform:'node',format:'esm',outfile:bundle,logLevel:'silent'})
  const {exercise,verifyRestored}=await import(pathToFileURL(bundle))
  const source=join(temporary,'data');const restored=join(temporary,'restored')
  if(process.env.TEST_LEGACY_INTEROP==='1'){
    const base=await start(source)
    execFileSync(process.execPath,[join(root,'tests/opaque-interop.mjs')],{cwd:temporary,env:{...process.env,TEST_BASE_URL:base},stdio:'inherit'})
    await stop()
  }
  const fixture=await exercise(await start(source));await stop()
  await cp(source,restored,{recursive:true,preserveTimestamps:true})
  const before=await manifest(restored)
  const report=JSON.parse(execFileSync(binary,['--check-data'],{cwd:temporary,env:environment(restored),encoding:'utf8'}))
  assert.equal(report.users,1);assert.equal(report.files,8)
  assert.deepEqual(await manifest(restored),before,'Read-only diagnosis mutated the backup')
  const indexKey=join(restored,'index/index.key');const keyBytes=await readFile(indexKey)
  await rm(indexKey)
  assert.throws(()=>execFileSync(binary,['--check-data'],{cwd:temporary,env:environment(restored),stdio:'pipe'}),'Missing key accepted')
  assert.ok(!(await readdir(join(restored,'index'))).includes('index.key'),'Diagnostic regenerated a key')
  await writeFile(indexKey,keyBytes,{mode:0o600})
  await chmod(indexKey,0o644)
  assert.throws(()=>execFileSync(binary,['--check-data'],{cwd:temporary,env:environment(restored),stdio:'pipe'}),'Public backup key accepted')
  assert.equal((await stat(indexKey)).mode&0o777,0o644,'Diagnostic changed permissions')
  await chmod(indexKey,0o600)
  const setupPath=join(restored,'opaque_server_setup');const setupBytes=await readFile(setupPath)
  await writeFile(setupPath,'invalid setup')
  assert.throws(()=>execFileSync(binary,['--check-data'],{cwd:temporary,env:environment(restored),stdio:'pipe'}),'Corrupt OPAQUE setup accepted')
  await writeFile(setupPath,setupBytes)
  await verifyRestored(await start(restored),fixture);await stop()
  console.log('Actual browser crypto/Go: password rewrap, VMK rotation, revocation, five collections and isolated backup restore passed')
}finally{await stop();await rm(temporary,{recursive:true,force:true})}
