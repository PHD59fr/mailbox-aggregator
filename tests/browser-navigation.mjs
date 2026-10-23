import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, dirname, extname } from 'node:path'
import { fileURLToPath } from 'node:url'
import assert from 'node:assert/strict'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const profile = await mkdtemp(join(tmpdir(), 'emailer-navigation-browser-'))
let mode = 'once', failures = 0, pageLoads = 0
const server = createServer(async (request, response) => {
  const path = new URL(request.url, 'http://localhost').pathname
  response.setHeader('Cache-Control', 'no-store')
  if (path.startsWith('/api/')) {
    response.setHeader('Content-Type', 'application/json')
    if (path.endsWith('/auth/options')) return response.end('{"registration_enabled":true}')
    response.writeHead(401); return response.end('{"error":{"code":"UNAUTHENTICATED","message":"Sign in."}}')
  }
  if (/\/RegisterView-[^/]+\.js$/.test(path) && mode !== 'healthy') {
    if (mode === 'always' || failures === 0) { failures++; response.writeHead(404); return response.end() }
  }
  if (path === '/register') pageLoads++
  const file = extname(path) ? path : '/index.html'
  if (file.includes('..')) { response.writeHead(404); return response.end() }
  try {
    const bytes = await readFile(join(root, 'web/dist', file))
    response.setHeader('Content-Type', ({ '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.wasm': 'application/wasm' })[extname(file)] || 'application/octet-stream')
    response.end(bytes)
  } catch { response.writeHead(404); response.end() }
})
let browser, socket
try {
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve) })
  const origin = `http://127.0.0.1:${server.address().port}`
  browser = spawn(process.env.CHROME_BIN || 'chromium', ['--headless', '--no-sandbox', '--disable-gpu', '--no-first-run', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { stdio: 'ignore' })
  let browserError
  browser.on('error', error => { browserError = error })
  async function waitFor(operation) {
    for (let attempt = 0; attempt < 300; attempt++) {
      if (browserError) throw browserError
      const result = await operation().catch(() => null)
      if (result) return result
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    throw new Error('Navigation browser test timed out')
  }
  const debugPort = await waitFor(async () => (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0])
  const targets = await (await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json()
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }) })
  let sequence = 0
  const pending = new Map()
  socket.addEventListener('message', ({ data }) => {
    const message = JSON.parse(data), task = pending.get(message.id)
    if (!task) return
    pending.delete(message.id); clearTimeout(task.timer)
    if (message.error) task.reject(new Error(message.error.message)); else task.resolve(message.result)
  })
  function call(method, params = {}) {
    const id = ++sequence
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { pending.delete(id); reject(new Error(`Browser command timed out: ${method}`)) }, 10000)
      pending.set(id, { resolve, reject, timer }); socket.send(JSON.stringify({ id, method, params }))
    })
  }
  async function evaluate(expression) {
    const result = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails))
    return result.result.value
  }
  async function openLogin() {
    await call('Page.navigate', { url: `${origin}/login` })
    await waitFor(() => evaluate('location.pathname === "/login" && !!document.querySelector("a[href=\\"/register\\"]")'))
    await evaluate('sessionStorage.clear()')
  }
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 320, height: 720, deviceScaleFactor: 1, mobile: true })
  await openLogin()
  await evaluate('document.querySelector("a[href=\\"/register\\"]").click()')
  await waitFor(() => evaluate('document.querySelector("h2")?.textContent === "Create your vault"'))
  assert.equal(failures, 1, 'The test did not simulate a missing lazy page')
  assert.equal(pageLoads, 1, 'The app did not reload the requested destination exactly once')

  mode = 'always'; failures = 0; pageLoads = 0
  await openLogin()
  await evaluate('document.querySelector("a[href=\\"/register\\"]").click()')
  await waitFor(() => evaluate('location.pathname === "/register" && !!document.querySelector(".navigation-error button")'))
  await new Promise(resolve => setTimeout(resolve, 400))
  assert.equal(pageLoads, 1, 'A permanently missing chunk caused a reload loop')
  assert.equal(failures, 2, 'Initial navigation and the single reload did not both fail')
  assert.ok(await evaluate('document.documentElement.scrollWidth <= innerWidth'), 'Mobile recovery UI overflows')
  mode = 'healthy'
  await evaluate('document.querySelector(".navigation-error button").click()')
  await waitFor(() => evaluate('document.querySelector("h2")?.textContent === "Create your vault"'))
  assert.equal(pageLoads, 2, 'Manual retry did not open the requested destination')
  // Authentication reuses the last profile language without exposing settings.
  assert.equal(await evaluate('document.querySelector(".language-settings")'),null,'Registration exposes user settings')
  await evaluate('localStorage.setItem("mailbox-interface-language-v1",JSON.stringify({locale:"fr",direction:"rtl"}))')
  await call('Page.reload')
  await waitFor(()=>evaluate('document.documentElement.lang === "fr" && document.documentElement.dir === "rtl" && document.querySelector("h2")?.textContent === "Créer votre coffre"'))
  assert.equal(await evaluate('document.querySelector(".language-settings")'),null)
  assert.ok(await evaluate('document.documentElement.scrollWidth <= innerWidth'),'French RTL mobile authentication overflows')
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})
  await waitFor(()=>evaluate('innerWidth === 1280'))
  assert.ok(await evaluate('document.querySelector(".auth-copy").getBoundingClientRect().x > document.querySelector(".auth-card").getBoundingClientRect().x'),'RTL authentication columns did not follow direction')
  await evaluate(`document.querySelector('input[autocomplete="username"]').focus()`)
  await call('Input.dispatchKeyEvent',{type:'keyDown',key:'Tab',code:'Tab',windowsVirtualKeyCode:9})
  await call('Input.dispatchKeyEvent',{type:'keyUp',key:'Tab',code:'Tab',windowsVirtualKeyCode:9})
  assert.ok(await evaluate('document.activeElement === document.querySelector(\'input[type="password"]\')'),'RTL authentication broke keyboard order')
  await evaluate('localStorage.setItem("mailbox-interface-language-v1",JSON.stringify({locale:"en",direction:"auto"}))')
  await call('Page.reload')
  await waitFor(()=>evaluate('document.documentElement.dir === "ltr" && document.querySelector("h2")?.textContent === "Create your vault"'))
  assert.ok(await evaluate('document.querySelector(".auth-copy").getBoundingClientRect().x < document.querySelector(".auth-card").getBoundingClientRect().x'),'LTR layout did not restore')
  await call('Page.navigate',{url:`${origin}/login`})
  await waitFor(()=>evaluate('location.pathname === "/login" && !!document.querySelector("form")'))
  assert.equal(await evaluate('document.querySelector(".language-settings")'),null,'Login exposes user settings')
  console.log('Chromium: lazy page recovery, login without settings, remembered profile language, RTL/mobile layout and keyboard order passed')
} finally {
  socket?.close()
  if (browser?.pid && browser.exitCode === null && browser.signalCode === null) {
    const exited = new Promise(resolve => browser.once('exit', resolve))
    browser.kill('SIGKILL'); await exited
  }
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
