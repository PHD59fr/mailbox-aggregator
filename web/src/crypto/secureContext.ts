import { APIError } from '../api/client'

export const HTTPS_REQUIRED_MESSAGE = 'A secure HTTPS connection is required. HTTP access to this server cannot securely open your vault. Open the app using its HTTPS address. Your username and password have not been checked.'

export function requiresHTTPS(): boolean {
  return globalThis.isSecureContext === false
}

export function requireSecureContext(): void {
  if (requiresHTTPS()) throw new APIError('HTTPS_REQUIRED', HTTPS_REQUIRED_MESSAGE, 400)
}
