import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './styles.css'
import './themes.css'
import { initializeTheme } from './stores/theme'
import { initializeLanguage } from './i18n'

initializeTheme()
initializeLanguage()
createApp(App).use(createPinia()).use(router).mount('#app')
