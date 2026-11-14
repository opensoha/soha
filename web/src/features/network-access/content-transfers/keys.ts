export const managedContentKeys = {
  network: (viewer: string, scope: string, id: string) =>
    ['managed-content', 'network', viewer, scope, id] as const,
  reviewDestinations: (viewer: string, scope: string) =>
    ['managed-content', 'review-destinations', viewer, scope] as const,
  all: ['managed-content'] as const,
  destinations: (viewer: string, scope: string, admin: boolean) =>
    ['managed-content', 'destinations', viewer, scope, admin] as const,
  records: (
    viewer: string,
    scope: string,
    kind: string,
    destination: string,
    offset: number,
    limit: number,
  ) => ['managed-content', 'records', viewer, scope, kind, destination, offset, limit] as const,
  detail: (viewer: string, scope: string, id: string) =>
    ['managed-content', 'detail', viewer, scope, id] as const,
}
