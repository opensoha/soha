# FRONTEND-PRO-PAGES

## Scope Completed

- Rebuilt the active `web` route surface onto direct ant-design-pro `pages/**` entry modules.
- Replaced the temporary catch-all `kubecrux/route-host` bridge with Pro-native route targets.
- Switched active auth runtime and shell redirects from `/user/login` to `/login`.
- Removed active scaffold shell references from the header/footer/runtime path.
- Removed active demo entry files `web/src/pages/Welcome*`, `web/src/pages/Admin.tsx`, and `web/src/pages/kubecrux/route-host.tsx`.

## Files Added

- `web/src/pages/login.tsx`
- `web/src/pages/overview.tsx`
- `web/src/pages/platform/clusters.tsx`
- `web/src/pages/platform/cluster-resources.tsx`
- `web/src/pages/platform/workloads.tsx`
- `web/src/pages/platform/configuration.tsx`
- `web/src/pages/platform/access-control.tsx`
- `web/src/pages/platform/network.tsx`
- `web/src/pages/platform/storage.tsx`
- `web/src/pages/platform/extensions.tsx`
- `web/src/pages/delivery.tsx`
- `web/src/pages/observability.tsx`
- `web/src/pages/ai-observe.tsx`
- `web/src/pages/access.tsx`
- `web/src/pages/system.tsx`
- `web/src/pages/settings.tsx`

## Files Updated

- `web/config/routes.ts`
- `web/config/routes.simple.ts`
- `web/src/app.tsx`
- `web/src/access.ts` (read/kept as-is for route access model)
- `web/src/components/Footer/index.tsx`
- `web/src/components/RightContent/AvatarDropdown.tsx`
- `web/src/components/index.ts`
- `web/src/features/auth/runtime.ts`
- `web/src/requestErrorConfig.ts`

## Validation

- `npm run build` fails before page compilation because `@umijs/max-plugin-openapi` cannot be resolved from `web/config/config.ts`.
- `npm run tsc` fails broadly due to existing workspace baseline issues unrelated to this track, including missing module/type resolution across the current Umi/web setup and legacy mock/demo files.

## Risks

- Some dormant scaffold files still exist under `web/src/pages/**` and locale/menu strings; they are no longer part of the active route surface, but they still contribute noise to repo cleanup and TypeScript output.
- Several top-level menu groupings in `web/config/routes.ts` are intentionally route-driven wrappers around existing feature exports; if menu IA changes again, both `routes.ts` and `routes/meta.ts` must stay aligned.
- Auth-related legacy tests and dormant register/demo pages still reference `/user/login`; they were not migrated in this track because they are outside the active Pro shell path.

## Recommended Next Step

- Normalize the `web` validation baseline first: remove or fix unresolved Umi plugin/dependency references and exclude or migrate legacy mock/demo test surfaces so `tsc` and `build` can validate the rebuilt Pro shell cleanly.
