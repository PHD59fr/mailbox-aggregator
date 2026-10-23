import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'
import { useSessionStore } from '../stores/session'
import { installNavigationRecovery } from './recovery'

export function createAppRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({ history, scrollBehavior: to => to.hash ? { el: to.hash } : { top: 0 }, routes: [
    { path: '/', redirect: '/mail' },
    { path: '/login', component: () => import('../views/LoginView.vue') },
    { path: '/register', component: () => import('../views/RegisterView.vue') },
    { path: '/mail', component: () => import('../views/MailView.vue'), meta: { requiresAuth: true } },
    { path: '/contacts', component: () => import('../views/ContactsView.vue'), meta: { requiresAuth: true } },
    { path: '/settings', component: () => import('../views/SettingsView.vue'), meta: { requiresAuth: true } },
    { path: '/compose', component: () => import('../views/ComposeView.vue'), meta: { requiresAuth: true }, props: route => ({ smtpConfigID: typeof route.query.from === 'string' ? route.query.from : undefined }) },
  ] })

  let restoringSession: Promise<void> | undefined
  installNavigationRecovery(router)
  router.beforeEach(async to => {
    const session = useSessionStore()
    if (session.loading) {
      restoringSession ??= session.restore().finally(() => { restoringSession = undefined })
      await restoringSession
    }
    if (to.meta.requiresAuth && !session.authenticated) {
      return { path: '/login', replace: true }
    }
  })

  return router
}

export const router = createAppRouter()
