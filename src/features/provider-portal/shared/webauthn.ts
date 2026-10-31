import type {
  MFAWebAuthnRequestOptions,
  MFAWebAuthnResponse,
} from '@opensoha/contracts/gen/ts/sohaapi'

export function decodeBase64Url(value: string) {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const bytes = atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '='))
  return Uint8Array.from(bytes, (character) => character.charCodeAt(0))
}

export function encodeBase64Url(value: ArrayBuffer) {
  let binary = ''
  new Uint8Array(value).forEach((byte) => {
    binary += String.fromCharCode(byte)
  })
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export async function requestPasskeyAssertion(
  options: MFAWebAuthnRequestOptions,
  signal?: AbortSignal,
): Promise<MFAWebAuthnResponse | null> {
  const credential = (await navigator.credentials.get({
    ...(signal ? { signal } : {}),
    publicKey: {
      challenge: decodeBase64Url(options.challenge),
      rpId: options.rpId,
      timeout: options.timeoutMilliseconds,
      userVerification: options.userVerification,
      allowCredentials: options.allowCredentialIds?.map((id) => ({
        id: decodeBase64Url(id),
        type: 'public-key',
      })),
    },
  })) as PublicKeyCredential | null
  if (!credential) return null
  const response = credential.response as AuthenticatorAssertionResponse
  return {
    credentialId: credential.id,
    clientDataJSON: encodeBase64Url(response.clientDataJSON),
    authenticatorData: encodeBase64Url(response.authenticatorData),
    signature: encodeBase64Url(response.signature),
    userHandle: response.userHandle ? encodeBase64Url(response.userHandle) : undefined,
  }
}
