import type { editor } from 'monaco-editor'
import { isMap, isNode, isScalar, isSeq, LineCounter, parseAllDocuments } from 'yaml'

export type YamlChange = {
  key: string
  path?: string
  before?: string
  after?: string
  line: number
}

type Field = { text: string; start: number; end: number }

/** Keep the differing portion visible in narrow rows, e.g. a long image's version. */
export function yamlValuePreview(value: string, comparison = '') {
  if (value.length <= 48) return value
  let prefix = 0
  while (prefix < value.length && value[prefix] === comparison[prefix]) prefix++
  const start = Math.max(0, prefix - 16)
  return `${start ? '…' : ''}${value.slice(start, start + 48)}${value.length > start + 48 ? '…' : ''}`
}

function fields(source: string) {
  const counter = new LineCounter()
  const documents = parseAllDocuments(source, { lineCounter: counter })
  if (documents.some((document) => document.errors.length)) return null
  const result = new Map<string, Field>()
  let supported = true
  const visit = (node: unknown, path: string) => {
    if (isMap(node) && node.items.length) {
      for (const item of node.items) {
        if (!isScalar(item.key)) {
          supported = false
          continue
        }
        const key = String(item.key.value)
        const segment = /^[a-zA-Z_][\w-]*$/.test(key)
          ? `${path ? '.' : ''}${key}`
          : `[${JSON.stringify(key)}]`
        visit(item.value, `${path}${segment}`)
      }
    } else if (isSeq(node) && node.items.length) {
      // Array paths follow YAML indices, including insertions and reordering.
      node.items.forEach((item, index) => visit(item, `${path}[${index}]`))
    } else if (isNode(node) && node.range) {
      result.set(path || '$', {
        text: source.slice(node.range[0], node.range[1]).trim(),
        start: counter.linePos(node.range[0]).line,
        end: counter.linePos(Math.max(node.range[0], node.range[1] - 1)).line,
      })
    }
  }
  documents.forEach((document, index) =>
    visit(document.contents, documents.length > 1 ? `$[${index}]` : ''),
  )
  return supported ? result : null
}

/** Native line hunks remain the fallback for invalid YAML, comments and formatting. */
export function yamlChangeReview(original: string, modified: string, hunks: editor.ILineChange[]) {
  const before = fields(original)
  const after = fields(modified)
  const changes: YamlChange[] = []
  const covered = new Set<editor.ILineChange>()
  const touches = (field: Field | undefined, start: number, end: number) =>
    field !== undefined && end !== 0 && field.start <= end && field.end >= start

  if (before && after) {
    for (const path of new Set([...before.keys(), ...after.keys()])) {
      const left = before.get(path)
      const right = after.get(path)
      if (left?.text === right?.text) continue
      const matching = hunks.filter(
        (hunk) =>
          touches(left, hunk.originalStartLineNumber, hunk.originalEndLineNumber) ||
          touches(right, hunk.modifiedStartLineNumber, hunk.modifiedEndLineNumber),
      )
      if (!matching.length) continue
      matching.forEach((hunk) => covered.add(hunk))
      changes.push({
        key: path,
        path,
        before: left?.text,
        after: right?.text,
        line: right?.start ?? Math.max(1, matching[0].modifiedStartLineNumber),
      })
    }
  }

  const oldLines = original.split('\n')
  const newLines = modified.split('\n')
  for (const [index, hunk] of hunks.entries()) {
    if (covered.has(hunk)) continue
    changes.push({
      key: `hunk:${index}`,
      before: hunk.originalEndLineNumber
        ? oldLines.slice(hunk.originalStartLineNumber - 1, hunk.originalEndLineNumber).join('\n')
        : undefined,
      after: hunk.modifiedEndLineNumber
        ? newLines.slice(hunk.modifiedStartLineNumber - 1, hunk.modifiedEndLineNumber).join('\n')
        : undefined,
      line: Math.max(1, hunk.modifiedStartLineNumber),
    })
  }
  return changes.sort((a, b) => a.line - b.line)
}
