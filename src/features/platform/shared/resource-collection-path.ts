/** Resource identity in a detail URL must not replace the selected list filter. */
export function resourceCollectionPath(
  collection: string,
  detailSearch: string,
  listNamespace: string | null,
) {
  const queryIndex = collection.indexOf('?')
  const pathname = queryIndex < 0 ? collection : collection.slice(0, queryIndex)
  const params = new URLSearchParams(
    queryIndex < 0 ? detailSearch : collection.slice(queryIndex + 1),
  )
  for (const key of ['tab', 'range', 'container', 'edit', 'webhook']) params.delete(key)
  if (listNamespace) params.set('namespace', listNamespace)
  else params.delete('namespace')
  return `${pathname || '/overview'}${params.size ? `?${params}` : ''}`
}
