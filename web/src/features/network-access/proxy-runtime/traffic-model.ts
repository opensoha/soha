import type { NetworkProxyTrafficSample } from '@opensoha/contracts/gen/ts/sohaapi'
function rateKnown(state: NetworkProxyTrafficSample['rateState']) {
  return state !== 'gap' && state !== 'unknown'
}
export function trafficSegments(
  samples: NetworkProxyTrafficSample[],
): NetworkProxyTrafficSample[][] {
  const segments: NetworkProxyTrafficSample[][] = []
  let previous: NetworkProxyTrafficSample | undefined
  for (const sample of samples) {
    const dt = previous ? Date.parse(sample.observedAt) - Date.parse(previous.observedAt) : 0
    const known =
      rateKnown(sample.rateState) &&
      sample.uploadBytesPerSecond != null &&
      sample.downloadBytesPerSecond != null
    const contiguous =
      previous &&
      sample.processEpoch === previous.processEpoch &&
      dt > 0 &&
      dt <= 60000 &&
      (sample.sampleSequence === undefined ||
        previous.sampleSequence === undefined ||
        sample.sampleSequence === previous.sampleSequence + 1)
    if (!known) {
      previous = undefined
      continue
    }
    if (!contiguous) segments.push([])
    segments[segments.length - 1].push(sample)
    previous = sample
  }
  return segments
}
export const rateText = (
  value: number | null | undefined,
  format: (value: number) => string,
  state?: NetworkProxyTrafficSample['rateState'],
) => (!rateKnown(state) || value == null ? '—' : `${format(value)}/s`)
