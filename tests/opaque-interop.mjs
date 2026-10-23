import assert from 'node:assert/strict'
import * as opaque from '../web/node_modules/@serenity-kit/opaque/esm/index.js'

const baseURL = process.env.TEST_BASE_URL ?? 'http://127.0.0.1:8080'
const login = `opaque-${Date.now()}@example.test`
const password = 'correct horse battery staple for OPAQUE'
const identifiers = { client: login, server: 'mail-aggregator' }

async function post(path, body, cookie, csrf) {
  const headers = { 'content-type': 'application/json' }
  if (cookie) headers.cookie = cookie
  if (csrf) headers['x-csrf-token'] = csrf
  const response = await fetch(`${baseURL}/api/v1${path}`, { method: 'POST', headers, body: JSON.stringify(body) })
  const value = await response.json().catch(() => ({}))
  assert.equal(response.ok, true, `${path}: ${response.status} ${JSON.stringify(value)}`)
  return { value, cookie: response.headers.get('set-cookie')?.split(';', 1)[0] }
}

await opaque.ready
const registration = opaque.client.startRegistration({ password })
const registerStart = await post('/auth/register/start', { login, registration_request: registration.registrationRequest })
const registrationResult = opaque.client.finishRegistration({
  clientRegistrationState: registration.clientRegistrationState,
  registrationResponse: registerStart.value.registration_response,
  password,
  identifiers,
})
const base64 = (length) => Buffer.alloc(length).toString('base64').replace(/=+$/, '')
const envelope = { version: 1, algorithm: 'AES-256-GCM', key_version: 1, nonce: base64(12), ciphertext: base64(32) }
const metadata = {
  version: 1, kdf: 'argon2id', kdf_salt: base64(16),
  kdf_parameters: { memory_kib: 65536, iterations: 3, parallelism: 1, key_length: 32 },
  wrap_algorithm: 'AES-256-GCM', wrapped_vmk: base64(48), wrap_nonce: base64(12), key_version: 1,
}
const files = Object.fromEntries(['config', 'accounts', 'identities', 'smtp', 'preferences'].map((name) => [name, envelope]))
const registrationDone = await post('/auth/register/finish', {
  flow_id: registerStart.value.flow_id,
  registration_record: registrationResult.registrationRecord,
  vault_metadata: metadata,
  files,
})
assert.equal(registrationDone.value.state, 'AUTHENTICATED_LOCKED')

const loginClient = opaque.client.startLogin({ password })
const loginStart = await post('/auth/login/start', { login, start_login_request: loginClient.startLoginRequest })
const loginResult = opaque.client.finishLogin({
  clientLoginState: loginClient.clientLoginState,
  loginResponse: loginStart.value.login_response,
  password,
  identifiers,
})
assert.ok(loginResult, 'browser OPAQUE client accepted Go server response')
const loginDone = await post('/auth/login/finish', {
  flow_id: loginStart.value.flow_id,
  finish_login_request: loginResult.finishLoginRequest,
})
assert.equal(loginDone.value.user_id, registrationDone.value.user_id)
assert.equal(loginDone.value.state, 'AUTHENTICATED_LOCKED')

// Activation only validates this synthetic public destination; it never dials
// the provider. Exercise the lock revision contract using the real HTTP server.
const activation = {
  lock_revision: loginDone.value.lock_revision,
  lifetime_seconds: 300,
  accounts: { test: { host: '8.8.8.8', port: 993, security: 'tls', username: 'synthetic', password: 'synthetic' } },
}
await post('/session/credentials', activation, loginDone.cookie, loginDone.value.csrf_token)
const locked = await fetch(`${baseURL}/api/v1/session/credentials`, {
  method: 'DELETE',
  headers: { cookie: loginDone.cookie, 'x-csrf-token': loginDone.value.csrf_token },
})
assert.equal(locked.status, 204)
const revision = Number(locked.headers.get('x-lock-revision'))
assert.equal(revision, activation.lock_revision + 1)
const stale = await fetch(`${baseURL}/api/v1/session/credentials`, {
  method: 'POST',
  headers: { 'content-type': 'application/json', cookie: loginDone.cookie, 'x-csrf-token': loginDone.value.csrf_token },
  body: JSON.stringify(activation),
})
assert.equal(stale.status, 409)
assert.equal((await stale.json()).error.code, 'LOCK_REVISION_CONFLICT')
await post('/session/credentials', { ...activation, lock_revision: revision }, loginDone.cookie, loginDone.value.csrf_token)

// Grouped configuration import checks revisions, preserves all three files
// and revokes the old session. Envelopes stay opaque to the Go server.
const imports = {}
for (const name of ['accounts', 'smtp', 'preferences']) {
  const response = await fetch(`${baseURL}/api/v1/vault/files/${name}`, { headers: { cookie: loginDone.cookie } })
  assert.equal(response.status, 200)
  imports[name] = { data: { ...envelope, nonce: Buffer.from(crypto.getRandomValues(new Uint8Array(12))).toString('base64').replace(/=+$/, '') }, revision: response.headers.get('x-resource-revision') }
}
const imported = await fetch(`${baseURL}/api/v1/vault/import`, {
  method: 'POST', headers: { 'content-type': 'application/json', cookie: loginDone.cookie, 'x-csrf-token': loginDone.value.csrf_token },
  body: JSON.stringify({ files: imports }),
})
assert.equal(imported.status, 204)
const expired = await fetch(`${baseURL}/api/v1/session`, { headers: { cookie: loginDone.cookie } })
assert.equal(expired.status, 401)
const refreshedClient = opaque.client.startLogin({ password })
const refreshedStart = await post('/auth/login/start', { login, start_login_request: refreshedClient.startLoginRequest })
const refreshedResult = opaque.client.finishLogin({ clientLoginState: refreshedClient.clientLoginState, loginResponse: refreshedStart.value.login_response, password, identifiers })
const refreshed = await post('/auth/login/finish', { flow_id: refreshedStart.value.flow_id, finish_login_request: refreshedResult.finishLoginRequest })
loginDone.cookie = refreshed.cookie
loginDone.value = refreshed.value
for (const name of ['accounts', 'smtp', 'preferences']) {
  const response = await fetch(`${baseURL}/api/v1/vault/files/${name}`, { headers: { cookie: loginDone.cookie } })
  assert.equal(response.status, 200)
  assert.deepEqual(await response.json(), imports[name].data)
}

const reauthClient = opaque.client.startLogin({ password })
const reauthStart = await post('/auth/reauth/start', { login, start_login_request: reauthClient.startLoginRequest }, loginDone.cookie, loginDone.value.csrf_token)
const reauthResult = opaque.client.finishLogin({
  clientLoginState: reauthClient.clientLoginState,
  loginResponse: reauthStart.value.login_response,
  password,
  identifiers,
})
assert.ok(reauthResult)
await post('/auth/reauth/finish', { flow_id: reauthStart.value.flow_id, finish_login_request: reauthResult.finishLoginRequest }, loginDone.cookie, loginDone.value.csrf_token)
const deleted = await fetch(`${baseURL}/api/v1/me`, {
  method: 'DELETE',
  headers: { 'content-type': 'application/json', cookie: loginDone.cookie, 'x-csrf-token': loginDone.value.csrf_token },
  body: JSON.stringify({ confirmation: 'DELETE' }),
})
assert.equal(deleted.status, 204, `self-deletion returned ${deleted.status}`)
console.log('OPAQUE browser/Go registration, login, lock revisions, grouped import and self-deletion interoperability passed')
