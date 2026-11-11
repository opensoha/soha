import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import type { IdentityApplication } from '@/features/identity'
import { providerPortalApi } from './api'
import { providerPortalKeys } from './keys'

export function invalidateProviderPortalQueries(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: providerPortalKeys.all })
}

export const providerPortalMutations = {
  revokeMFACredential: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'revoke-mfa'] as const,
      mutationFn: providerPortalApi.revokeMFACredential,
      onSuccess: () =>
        queryClient.invalidateQueries({ queryKey: providerPortalKeys.mfaCredentials() }),
    }),
  beginTOTPEnrollment: () =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'totp-enroll'] as const,
      mutationFn: providerPortalApi.beginTOTPEnrollment,
    }),
  verifyMFAChallenge: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'mfa-verify'] as const,
      mutationFn: ({ challengeId, response }: { challengeId: string; response: string }) =>
        providerPortalApi.verifyMFAChallenge(challengeId, response),
      onSuccess: () => invalidateProviderPortalQueries(queryClient),
    }),
  beginRecoveryChallenge: () =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'recovery-challenge'] as const,
      mutationFn: providerPortalApi.beginRecoveryChallenge,
    }),
  beginWebAuthnEnrollment: () =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'webauthn-enroll'] as const,
      mutationFn: providerPortalApi.beginWebAuthnEnrollment,
    }),
  beginWebAuthnAuthentication: () =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'webauthn-authenticate'] as const,
      mutationFn: (input: Parameters<typeof providerPortalApi.beginWebAuthnAuthentication>[0]) =>
        providerPortalApi.beginWebAuthnAuthentication(input),
    }),
  verifyWebAuthnChallenge: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'webauthn-verify'] as const,
      mutationFn: ({
        challengeId,
        response,
      }: {
        challengeId: string
        response: Parameters<typeof providerPortalApi.verifyWebAuthnChallenge>[1]
      }) => providerPortalApi.verifyWebAuthnChallenge(challengeId, response),
      onSuccess: () => invalidateProviderPortalQueries(queryClient),
    }),
  regenerateRecoveryCodes: () =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'recovery-codes'] as const,
      mutationFn: providerPortalApi.regenerateRecoveryCodes,
    }),
  launch: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'launch'] as const,
      mutationFn: (application: IdentityApplication) => providerPortalApi.launch(application.id),
      onSuccess: (decision) =>
        decision.launchUrl ? invalidateProviderPortalQueries(queryClient) : undefined,
    }),
  toggleFavorite: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: [...providerPortalKeys.all, 'mutation', 'favorite'] as const,
      mutationFn: async (application: IdentityApplication): Promise<void> => {
        if (application.favorite) {
          await providerPortalApi.unfavorite(application.id)
          return
        }
        await providerPortalApi.favorite(application.id)
      },
      onSuccess: () => invalidateProviderPortalQueries(queryClient),
    }),
}
