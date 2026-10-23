import { onMounted, ref } from 'vue'
import { api } from '../api/client'
import { requiresHTTPS } from '../crypto/secureContext'

export function useAuthOptions() {
  const registrationEnabled = ref(false)
  const optionsLoaded = ref(false)
  onMounted(async () => {
    if (requiresHTTPS()) return
    try { registrationEnabled.value = (await api<{ registration_enabled: boolean }>('/auth/options')).registration_enabled === true }
    catch { registrationEnabled.value = false }
    finally { optionsLoaded.value = true }
  })
  return { registrationEnabled, optionsLoaded }
}
