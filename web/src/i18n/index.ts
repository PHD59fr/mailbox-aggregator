import { computed, ref, watch } from 'vue'
import english from './en.json'
import french from './fr.json'

export type Locale = 'en' | 'fr'
export type Direction = 'auto' | 'ltr' | 'rtl'
export const locale = ref<Locale>('en')
export const direction = ref<Direction>('auto')
export const interfaceDirection = computed(() => direction.value === 'rtl' ? 'rtl' : 'ltr')
const catalogs: Record<Locale, Record<string, string>> = {en: english, fr: french}
const deviceKey = 'mailbox-interface-language-v1'

export function t(message: string | number | null | undefined, params: Record<string, unknown> = {}): string {
  const source = String(message ?? '')
  const catalog = catalogs[locale.value]
  const text = Object.prototype.hasOwnProperty.call(catalog, source) ? catalog[source]! : source
  return text.replace(/\{(\w+)\}/g, (token, key: string) => Object.prototype.hasOwnProperty.call(params, key) ? (typeof params[key] === 'number' ? formatNumber(params[key] as number) : String(params[key])) : token)
}
export function plural(one: string, many: string, count: number, params: Record<string, unknown> = {}) {
  return t(new Intl.PluralRules(locale.value).select(count) === 'one' ? one : many, {...params, count: formatNumber(count)})
}
export function formatNumber(value: number, options?: Intl.NumberFormatOptions) { return new Intl.NumberFormat(locale.value, options).format(value) }
export function formatDate(value: string | number, options: Intl.DateTimeFormatOptions = {dateStyle:'long',timeStyle:'short'}) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : new Intl.DateTimeFormat(locale.value, options).format(date)
}
export function setLanguage(value: Locale, flow: Direction = 'auto', remember = true) {
  locale.value = value === 'fr' ? 'fr' : 'en'
  direction.value = ['auto','ltr','rtl'].includes(flow) ? flow : 'auto'
  if (remember) {
    try { localStorage.setItem(deviceKey, JSON.stringify({locale:locale.value, direction:direction.value})) } catch { /* Optional device preference. */ }
  }
}
export function initializeLanguage() {
  try {
    const saved = JSON.parse(localStorage.getItem(deviceKey) || 'null') as {locale?:unknown;direction?:unknown} | null
    if (saved && ['en','fr'].includes(String(saved.locale))) setLanguage(saved.locale as Locale, saved.direction as Direction, false)
  } catch { /* English remains the default. */ }
}
watch([locale,interfaceDirection], () => {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = locale.value
    document.documentElement.dir = interfaceDirection.value
  }
}, {immediate:true,flush:'sync'})
