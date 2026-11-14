import type { CompanionRenderManifest, CompanionVisualState } from './types'

export interface CompanionInteractionMotion {
  animation: string
  sequence: number
}

export function companionStateAnimation(
  manifest: CompanionRenderManifest,
  state: CompanionVisualState,
) {
  if (state === 'disabled') return undefined
  return manifest.animations?.[state]
}

export function companionInteractionAnimation(
  manifest: CompanionRenderManifest,
  interactionId: string,
) {
  const interaction = manifest.interactions?.find((item) => item.id === interactionId)
  if (interaction?.animation) return interaction.animation
  return interactionId === 'tap' ? manifest.animations?.tap : undefined
}

export function companionInteractionId(
  manifest: CompanionRenderManifest,
  requestedInteraction: string | null | undefined,
) {
  return requestedInteraction &&
    manifest.interactions?.some((item) => item.id === requestedInteraction)
    ? requestedInteraction
    : 'tap'
}
