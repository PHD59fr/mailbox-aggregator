import { shallowRef } from 'vue'
import type { Router } from 'vue-router'

export const navigationFailure = shallowRef<{ path: string } | null>(null)
const retryKey = 'mailbox-asset-reload-v1'

export function reloadFailedPage() {
  if (navigationFailure.value) window.location.assign(navigationFailure.value.path)
}

export function installNavigationRecovery(router: Router, navigate = (path: string) => window.location.assign(path)) {
  let reloading = false
  router.onError((error, to) => {
    if (!/Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS/i.test(error.message)) return
    navigationFailure.value = { path: to.fullPath }
    if (reloading || navigator.onLine === false) return
    // Retain this marker across the reload, including failed initial navigation.
    // A missing/broken release must never cause a refresh loop. If sessionStorage
    // is blocked, keep the retry button instead of reloading without a guard.
    const build = document.querySelector<HTMLScriptElement>('script[type="module"][src]')?.src || window.location.origin
    try {
      if (sessionStorage.getItem(retryKey) === build) return
      sessionStorage.setItem(retryKey, build)
    } catch { return }
    reloading = true
    navigate(to.fullPath)
  })
  router.afterEach((_to, _from, failure) => {
    if (!failure) navigationFailure.value = null
  })
}
