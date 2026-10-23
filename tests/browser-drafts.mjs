import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import net from 'node:net'
import assert from 'node:assert/strict'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const profile = await mkdtemp(join(tmpdir(), 'emailer-draft-browser-'))
const reserve = net.createServer()
await new Promise(resolve => reserve.listen(0, '127.0.0.1', resolve))
const port = reserve.address().port
await new Promise(resolve => reserve.close(resolve))
const vite = spawn(process.execPath, [join(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', String(port), '--strictPort'], { cwd: join(root, 'web'), stdio: 'ignore' })
const browser = spawn(process.env.CHROME_BIN || 'chromium', ['--headless', '--no-sandbox', '--disable-gpu', '--no-first-run', '--remote-debugging-port=0', '--remote-debugging-address=127.0.0.1', `--user-data-dir=${profile}`, 'about:blank'], { stdio: 'ignore' })
let socket
const failures = []
for (const process of [vite, browser]) process.on('error', error => failures.push(error))
const pause = () => new Promise(resolve => setTimeout(resolve, 100))
async function waitFor(operation) {
  for (let attempt = 0; attempt < 300; attempt++) {
    if (failures.length) throw failures[0]
    try { const value = await operation(); if (value) return value } catch { /* The isolated services are still starting. */ }
    await pause()
  }
  throw new Error('Browser test timed out')
}
try {
  await waitFor(async () => (await fetch(`http://127.0.0.1:${port}/browser-tests/drafts.html`)).ok)
  const debugPort = await waitFor(async () => (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0])
  const targets = await (await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json()
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }) })
  const pending = new Map()
  let sequence = 0
  socket.addEventListener('message', ({ data }) => {
    const message = JSON.parse(data)
    if (!message.id) return
    const task = pending.get(message.id)
    if (!task) return
    pending.delete(message.id)
    if (message.error) task.reject(new Error(message.error.message)); else task.resolve(message.result)
  })
  function call(method, params = {}) {
    const id = ++sequence
    return new Promise((resolve, reject) => { pending.set(id, { resolve, reject }); socket.send(JSON.stringify({ id, method, params })) })
  }
  await call('Page.navigate', { url: `http://127.0.0.1:${port}/browser-tests/drafts.html` })
  const outcome = await waitFor(async () => {
    const { result } = await call('Runtime.evaluate', { expression: `JSON.stringify({status:document.getElementById('result')?.dataset.status,text:document.getElementById('result')?.textContent})`, returnByValue: true })
    const value = JSON.parse(result.value || '{}')
    return value.status ? value : null
  })
  assert.equal(outcome.status, 'passed', outcome.text)
  console.log('Chromium: encrypted drafts/attachments, VMK migration, concurrency and responsive/cancellable Argon2 Worker passed')
} finally {
  socket?.close()
  for (const child of [browser, vite]) {
    const exited = new Promise(resolve => child.once('exit', resolve))
    if (child.pid && child.exitCode === null && child.signalCode === null) { child.kill('SIGKILL'); await exited }
  }
  // Wait a bit for file handles to be released on the profile directory
  await new Promise(resolve => setTimeout(resolve, 500))
  await rm(profile, { recursive: true, force: true })
}
