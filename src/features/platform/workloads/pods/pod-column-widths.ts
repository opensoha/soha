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
