import { describe, expect, it } from 'vitest'
import type { editor } from 'monaco-editor'
import { yamlChangeReview, yamlValuePreview } from './yaml-change-review'

function hunk(
  start: number,
  end = start,
  modifiedStart = start,
  modifiedEnd = end,
): editor.ILineChange {
  return {
    originalStartLineNumber: start,
    originalEndLineNumber: end,
    modifiedStartLineNumber: modifiedStart,
    modifiedEndLineNumber: modifiedEnd,
    charChanges: undefined,
  }
}

describe('YAML change directory', () => {
  it('keeps the changed version visible when an image reference has a long common prefix', () => {
    const prefix = 'registry.k8s.io/kube-state-metrics/kube-state-metrics:'
    expect(
      yamlValuePreview(prefix + 'v2.20.0-with-a-long-tag', prefix + 'v2.21.0-with-a-long-tag'),
    ).toContain('v2.20.0')
    expect(yamlValuePreview('256Mi', '512Mi')).toBe('256Mi')
  })
  it('maps scalar changes and array paths to their editable lines', () => {
    const old =
      'spec:\n  replicas: 1\n  containers:\n    - image: nginx:1\n      resources:\n        limits:\n          memory: 128Mi'
    const next = old
      .replace('replicas: 1', 'replicas: 3')
      .replace('nginx:1', 'nginx:2')
      .replace('128Mi', '256Mi')
    expect(yamlChangeReview(old, next, [hunk(2), hunk(4), hunk(7)])).toEqual([
      { key: 'spec.replicas', path: 'spec.replicas', before: '1', after: '3', line: 2 },
      {
        key: 'spec.containers[0].image',
        path: 'spec.containers[0].image',
        before: 'nginx:1',
        after: 'nginx:2',
        line: 4,
      },
      {
        key: 'spec.containers[0].resources.limits.memory',
        path: 'spec.containers[0].resources.limits.memory',
        before: '128Mi',
        after: '256Mi',
        line: 7,
      },
    ])
  })

  it('distinguishes missing fields, nulls and empty strings, with deletion anchors', () => {
    expect(
      yamlChangeReview('a: null\nb: ""', 'b: ""\nc: false', [hunk(1, 1, 0, 0), hunk(2, 0, 2, 2)]),
    ).toEqual([
      { key: 'a', path: 'a', before: 'null', after: undefined, line: 1 },
      { key: 'c', path: 'c', before: undefined, after: 'false', line: 2 },
    ])
  })

  it('preserves punctuation in keys, multiline values and alias syntax without resolving aliases', () => {
    const old = 'annotations:\n  foo.bar/key: |\n    hello\n    world\na: &x 1\nb: *x'
    const next = old.replace('world', 'everyone').replace('*x', '2')
    const changes = yamlChangeReview(old, next, [hunk(4), hunk(6)])
    expect(changes[0]).toMatchObject({
      path: 'annotations["foo.bar/key"]',
      before: '|\n    hello\n    world',
      after: '|\n    hello\n    everyone',
      line: 2,
    })
    expect(changes[1]).toMatchObject({ path: 'b', before: '*x', after: '2', line: 6 })
  })

  it.each([
    ['a: 1\n# old', 'a: 1\n# new'],
    ['a: 1\nb: 2', 'a: 1\nb: ['],
    ['a:\n  b: 1', 'a:\n    b: 1'],
  ])('keeps native text changes when fields cannot describe the edit', (old, next) => {
    const changes = yamlChangeReview(old, next, [
      hunk(2, old.split('\n').length, 2, next.split('\n').length),
    ])
    expect(changes).toHaveLength(1)
    expect(changes[0].path).toBeUndefined()
    expect(changes[0].after).toBe(next.split('\n').slice(1).join('\n'))
  })

  it('keeps document paths distinct in valid multi-document YAML', () => {
    expect(yamlChangeReview('a: 1\n---\na: 2', 'a: 1\n---\na: 3', [hunk(3)])).toEqual([
      { key: '$[1].a', path: '$[1].a', before: '2', after: '3', line: 3 },
    ])
  })

  it('includes separate comment-only hunks alongside semantic changes', () => {
    expect(
      yamlChangeReview('a: 1\nb: 2\n# old', 'a: 3\nb: 2\n# new', [hunk(1), hunk(3)]).map(
        (item) => item.path,
      ),
    ).toEqual(['a', undefined])
  })
})
