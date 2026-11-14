import { useEffect, useState } from 'react'

function initialDraft(resourceKey: string, serverValue?: string, storageKey?: string | null) {
  return {
    resourceKey,
    observed: serverValue,
    original: serverValue,
    value: (storageKey ? window.localStorage.getItem(storageKey) : null) ?? serverValue ?? '',
  }
}

// Shared by existing-resource editors; creation forms keep their own template state.
export function useYamlDraft(
  resourceKey: string,
  serverValue?: string,
  storageKey?: string | null,
) {
  const [state, setState] = useState(() => initialDraft(resourceKey, serverValue, storageKey))

  useEffect(() => {
    setState((current) => {
      if (current.resourceKey !== resourceKey || current.original === undefined)
        return initialDraft(resourceKey, serverValue, storageKey)
      if (serverValue === undefined || current.observed === serverValue) return current
      const clean = current.value === current.original || current.value === serverValue
      return {
        ...current,
        observed: serverValue,
        original: clean ? serverValue : current.original,
        value: clean ? serverValue : current.value,
      }
    })
  }, [resourceKey, serverValue, storageKey])

  const active = state.resourceKey === resourceKey
  return {
    resourceKey,
    value: active ? state.value : '',
    original: active ? state.original : undefined,
    serverChanged:
      active &&
      state.observed !== undefined &&
      state.original !== undefined &&
      state.observed !== state.original,
    setValue: (value: string) =>
      setState((current) =>
        current.resourceKey === resourceKey ? { ...current, value } : current,
      ),
    reset: () => {
      if (storageKey) window.localStorage.removeItem(storageKey)
      setState({
        resourceKey,
        observed: serverValue,
        original: serverValue,
        value: serverValue ?? '',
      })
    },
    compareLatest: () =>
      setState((current) =>
        current.resourceKey === resourceKey
          ? { ...current, observed: serverValue, original: serverValue }
          : current,
      ),
    applied: (value?: string) => {
      if (storageKey) window.localStorage.removeItem(storageKey)
      if (value === undefined) return
      setState((current) =>
        current.resourceKey === resourceKey
          ? { ...current, observed: value, original: value, value }
          : current,
      )
    },
  }
}
