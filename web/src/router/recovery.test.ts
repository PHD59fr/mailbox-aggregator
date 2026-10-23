import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installNavigationRecovery, navigationFailure } from './recovery'

beforeEach(() => {
  sessionStorage.clear()
  navigationFailure.value = null
  const script = document.createElement('script')
  script.type = 'module'; script.src = '/assets/index-release-a.js'; script.id = 'test-release'
  document.head.append(script)
})
afterEach(() => {
  document.getElementById('test-release')?.remove()
  vi.restoreAllMocks()
})

function fixture(message = 'Failed to fetch dynamically imported module: /assets/ContactsView-old.js') {
  const failure = new TypeError(message)
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/mail', component: { template: '<p>Inbox</p>' } },
    { path: '/contacts', component: () => Promise.reject(failure) },
  ] })
  const navigate = vi.fn()
  installNavigationRecovery(router, navigate)
  return { router, navigate, failure }
}

describe('navigation recovery after deployment', () => {
  it('reloads the requested URL once, including its query and hash', async () => {
    const { router, navigate, failure } = fixture()
    await router.push('/mail')
    await expect(router.push('/contacts?group=work#directory')).rejects.toBe(failure)
    expect(navigate).toHaveBeenCalledExactlyOnceWith('/contacts?group=work#directory')
    await expect(router.push('/contacts')).rejects.toBe(failure)
    expect(navigate).toHaveBeenCalledTimes(1)
  })

  it('does not enter a reload loop when the chunk still fails in the new document', async () => {
    const original = fixture()
    await expect(original.router.push('/contacts')).rejects.toBe(original.failure)
    const reloaded = fixture()
    await expect(reloaded.router.push('/contacts')).rejects.toBe(reloaded.failure)
    expect(reloaded.navigate).not.toHaveBeenCalled()
    expect(navigationFailure.value?.path).toBe('/contacts')
    await reloaded.router.push('/mail')
    expect(navigationFailure.value).toBeNull()
  })

  it('allows a recovery again when a different application version loads', async () => {
    const original = fixture()
    await expect(original.router.push('/contacts')).rejects.toBe(original.failure)
    document.getElementById('test-release')!.setAttribute('src', '/assets/index-release-b.js')
    const updated = fixture()
    await expect(updated.router.push('/contacts')).rejects.toBe(updated.failure)
    expect(updated.navigate).toHaveBeenCalledExactlyOnceWith('/contacts')
  })

  it.each(['Importing a module script failed.', 'error loading dynamically imported module: /assets/ContactsView.js', 'Unable to preload CSS for /assets/ContactsView.css'])('handles browser and stylesheet failures: %s', async message => {
    const { router, navigate, failure } = fixture(message)
    await expect(router.push('/contacts')).rejects.toBe(failure)
    expect(navigate).toHaveBeenCalledExactlyOnceWith('/contacts')
  })

  it('keeps a visible retry when offline', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
    const { router, navigate, failure } = fixture()
    await expect(router.push('/contacts')).rejects.toBe(failure)
    expect(navigate).not.toHaveBeenCalled()
    expect(navigationFailure.value?.path).toBe('/contacts')
  })

  it('does not reload without a persistent loop guard when storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('Storage blocked') })
    const { router, navigate, failure } = fixture()
    await expect(router.push('/contacts')).rejects.toBe(failure)
    expect(navigate).not.toHaveBeenCalled()
    expect(navigationFailure.value?.path).toBe('/contacts')
  })

  it('does not reload or hide programming errors', async () => {
    const { router, navigate, failure } = fixture('Cannot read properties of undefined')
    await expect(router.push('/contacts')).rejects.toBe(failure)
    expect(navigate).not.toHaveBeenCalled()
    expect(navigationFailure.value).toBeNull()
  })
})
