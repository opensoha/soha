import { providerPortalApi } from './api'
export { requestPasskeyAssertion } from './shared/webauthn'

// Public feature surface for step-up consumers; enrollment and provider management
// stay with the portal security page.
export const providerPortalMFA = {
  credentials: providerPortalApi.mfaCredentials,
  beginTOTP: providerPortalApi.beginTOTPEnrollment,
  beginRecovery: providerPortalApi.beginRecoveryChallenge,
  verify: providerPortalApi.verifyMFAChallenge,
  beginPasskey: () => providerPortalApi.beginWebAuthnAuthentication({ purpose: 'step_up' }),
  verifyPasskey: providerPortalApi.verifyWebAuthnChallenge,
}
