// @vitest-environment node
import { webcrypto } from 'node:crypto'
import { it, expect } from 'vitest'
import { encryptExportBundle, decryptExportBundle, type ExportBundle } from './useImportExport'
it('authenticates the export using the real Argon2id and AES-GCM implementations', async () => {
  Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true })
  const data: ExportBundle = { version: 1, exported_at: new Date().toISOString(), accounts: {}, smtp: {}, preferences: {} }
  const encoded = await encryptExportBundle(data, 'long export password')
  expect(await decryptExportBundle(encoded, 'long export password')).toEqual(data)
  await expect(decryptExportBundle(encoded, 'incorrect export password')).rejects.toThrow()
}, 30000)
