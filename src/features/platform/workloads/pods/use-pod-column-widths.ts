import { useEffect, useMemo, useRef, useState } from 'react'
import type { Pod } from './types'
import { podResourceColumnWidth } from './pod-resource-cell'

export function podContentWidths(pods: readonly Pod[], zh: boolean) {
  const textWidth = (values: string[], min: number, max: number) =>
    Math.min(
      max,
      values.reduce((largest, value) => Math.max(largest, value.length * 7 + 32), min),
    )
  return {
    namespace: textWidth(
      pods.map((pod) => pod.namespace),
      112,
      176,
    ),
    nodeName: textWidth(
      pods.map((pod) => pod.nodeName || ''),
      160,
      240,
    ),
    podIp: textWidth(
      pods.map((pod) => pod.podIp || ''),
      104,
      288,
    ),
    readyContainers: pods.reduce(
      (largest, pod) => {
        const count = pod.containerStatuses?.length ?? 1
        return Math.max(largest, Math.min(count, 6) * 12 + (count > 6 ? 16 : 0) + 24)
      },
      zh ? 80 : 112,
    ),
    cpu: podResourceColumnWidth('cpu', pods),
    memory: podResourceColumnWidth('memory', pods),
  }
}

export function usePodColumnWidths(
  pods: readonly Pod[],
  scope: string,
  zh: boolean,
  allNamespaces: boolean,
  selectable: boolean,
) {
  const ref = useRef<HTMLDivElement>(null)
  const [available, setAvailable] = useState(1100)
  const measured = useMemo(() => podContentWidths(pods, zh), [pods, zh])
  const [fitted, setFitted] = useState({ scope, initialized: false, widths: measured })
  useEffect(() => {
    if (fitted.scope !== scope || (!fitted.initialized && pods.length)) {
      setFitted({ scope, initialized: pods.length > 0, widths: measured })
    }
  }, [scope, measured, pods.length, fitted.scope, fitted.initialized])
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const measure = () => setAvailable(Math.max(320, element.clientWidth - 32))
    measure()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    observer?.observe(element)
    return () => observer?.disconnect()
  }, [])
  const widths = fitted.scope === scope ? fitted.widths : measured
  const core =
    (allNamespaces ? widths.namespace : 0) +
    88 +
    widths.readyContainers +
    64 +
    widths.cpu +
    widths.memory +
    48 +
    (selectable ? 44 : 0)
  return {
    ref,
    pinName: available >= 640,
    widths: { ...widths, name: Math.max(260, Math.min(360, available - core)) },
    fit: () => setFitted({ scope, initialized: pods.length > 0, widths: measured }),
  }
}
