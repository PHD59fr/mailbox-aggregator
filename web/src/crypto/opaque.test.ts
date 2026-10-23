import { describe, expect, it } from 'vitest'
import * as opaque from '@serenity-kit/opaque'

describe('RFC 9807 browser client', () => {
  it('creates client-side registration and login states without transmitting the password', async () => {
    await opaque.ready
    const password = 'correct horse battery staple - private'
    const registration = opaque.client.startRegistration({ password })
    const login = opaque.client.startLogin({ password })
    expect(registration.registrationRequest).toMatch(/^[A-Za-z0-9_-]+$/)
    expect(login.startLoginRequest).toMatch(/^[A-Za-z0-9_-]+$/)
    expect(registration.registrationRequest).not.toContain(password)
    expect(login.startLoginRequest).not.toContain(password)
    expect(registration.clientRegistrationState).toBeTruthy()
    expect(login.clientLoginState).toBeTruthy()
  })
})
