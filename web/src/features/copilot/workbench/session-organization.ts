export const PINNED_SESSION_TAG = 'soha:pinned'
export function sessionTagsWithPin(tags: string[] = [], pinned: boolean) {
  return [
    // Drop retired project labels on save and branch creation; retain other tags.
    ...tags.filter((tag) => tag !== PINNED_SESSION_TAG && !tag.startsWith('soha:project:')),
    ...(pinned ? [PINNED_SESSION_TAG] : []),
  ]
}
