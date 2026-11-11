interface ClaimPod {
  name: string
  namespace: string
  nodeName?: string
}

export interface PersistentVolumeClaimLifecycleInput {
  status: string
  volumeName?: string
  accessModes?: string[]
  pods?: ClaimPod[]
  podsTruncated?: boolean
}

export interface PersistentVolumeClaimAssessment {
  currentStep: number
  risks: Array<'lost' | 'pending' | 'multi-node-rwo' | 'truncated'>
}

export function assessPersistentVolumeClaim(
  claim: PersistentVolumeClaimLifecycleInput,
): PersistentVolumeClaimAssessment {
  const pods = claim.pods ?? []
  const risks: PersistentVolumeClaimAssessment['risks'] = []
  if (claim.status === 'Lost') risks.push('lost')
  if (claim.status === 'Pending') risks.push('pending')
  if (claim.podsTruncated) risks.push('truncated')
  const nodes = new Set(pods.map((pod) => pod.nodeName).filter(Boolean))
  if (claim.accessModes?.includes('ReadWriteOnce') && nodes.size > 1) risks.push('multi-node-rwo')
  return {
    currentStep: claim.status === 'Bound' ? (pods.length > 0 ? 2 : 1) : 0,
    risks,
  }
}
