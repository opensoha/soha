import { canAccessRoute, routeMeta } from '@/routes/meta';
import type { PermissionSnapshot } from '@/types';

export interface KubecruxInitialState {
  currentUser?: API.CurrentUser;
  permissionSnapshot?: PermissionSnapshot;
}

export default function access(initialState: KubecruxInitialState | undefined) {
  const snapshot = initialState?.permissionSnapshot;

  return new Proxy(
    {},
    {
      get(_target, prop) {
        if (typeof prop !== 'string') return false;
        if (!prop.startsWith('route:')) return false;

        const routeId = prop.slice('route:'.length);

        if (routeId === 'login' || routeId === 'oidc-callback' || routeId === 'login-callback') {
          return true;
        }

        const route = routeMeta.find((item) => item.id === routeId);
        if (!route) return false;
        return canAccessRoute(route, snapshot);
      },
    },
  ) as Record<string, boolean>;
}
