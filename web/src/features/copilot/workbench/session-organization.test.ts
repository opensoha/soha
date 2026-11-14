import { expect, it } from 'vitest'
import { sessionTagsWithPin, PINNED_SESSION_TAG } from './session-organization'
it('retires project labels while preserving pinning and unrelated tags', () => {
  const tags = sessionTagsWithPin(['user-tag', 'soha:project:Old', PINNED_SESSION_TAG], true)
  expect(tags).toEqual(['user-tag', PINNED_SESSION_TAG])
  expect(sessionTagsWithPin(tags, false)).toEqual(['user-tag'])
  expect(sessionTagsWithPin(['soha:project:Old', PINNED_SESSION_TAG, 'P1'], false)).toEqual(['P1'])
})
