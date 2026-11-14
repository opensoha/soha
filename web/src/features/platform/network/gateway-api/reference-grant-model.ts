/** Display-only projection of the existing list DTO's formatted references. */
export function grantSummaryParts(value: string) {
  const match = /^([^:]+):([^/]+)(?:\/(.+))?$/.exec(value)
  if (!match) return undefined
  const [, namespace, type, name] = match
  const dot = type.indexOf('.')
  const kind = dot < 0 ? type : type.slice(0, dot)
  const group = dot < 0 ? 'core' : type.slice(dot + 1)
  return { namespace, kind, group, name }
}

export function grantSummaryLabel(value: string, direction: 'from' | 'to', allNames: string) {
  const parts = grantSummaryParts(value)
  if (!parts) return value
  const { namespace, kind, group, name } = parts
  return direction === 'from'
    ? `${namespace} · ${kind} · ${group}`
    : `${kind} · ${name || allNames} · ${group}`
}
