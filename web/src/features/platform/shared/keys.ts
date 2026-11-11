export const platformSharedKeys = {
  resourceGitOpsStatus: (clusterId: string, namespace: string, kind: string, name: string) =>
    [
      'platform',
      'gitops-status',
      clusterId.trim(),
      namespace.trim(),
      kind.trim().toLowerCase(),
      name.trim(),
    ] as const,
}
