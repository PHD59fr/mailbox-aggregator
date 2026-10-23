// @vitest-environment node
import { webcrypto } from 'node:crypto'
import { beforeAll, describe, expect, it } from 'vitest'
import { createVault, decryptFile, encryptFile, unlockVault } from './vault'

beforeAll(() => { Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true }) })

describe('browser vault', () => {
  it('wraps the VMK and authenticates independently encrypted files', async () => {
    const userID = 'f4e526bd-11df-4d31-a838-67087e5526cf'
    const { metadata, vmk } = await createVault('correct horse battery staple', userID)
    const unlocked = await unlockVault('correct horse battery staple', userID, metadata)
    expect(unlocked).toEqual(vmk)
    const encrypted = await encryptFile({ accounts: [{ id: 'one' }] }, unlocked, userID, 'accounts')
    await expect(decryptFile(encrypted, unlocked, userID, 'accounts')).resolves.toEqual({ accounts: [{ id: 'one' }] })
    encrypted.ciphertext = encrypted.ciphertext.slice(0, -2) + 'AA'
    await expect(decryptFile(encrypted, unlocked, userID, 'accounts')).rejects.toThrow()
  }, 30_000)

  it('rejects an incorrect password', async () => {
    const userID = '439b7253-4b5a-4ef3-baba-4413eb71d5f7'
    const { metadata } = await createVault('a sufficiently long password', userID)
    await expect(unlockVault('the wrong password', userID, metadata)).rejects.toThrow()
  }, 30_000)
})
