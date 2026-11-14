# Soha Web Architecture And Development Standard

Use this reference for changes to `src/features`, `src/routes`, API/query/mutation code, shared
types or state, CSS ownership, lazy loading, architecture checks, and Graphify baselines.

## 1. Ownership Model

The active frontend is `soha-web`, built with Vite, React, React Router, Ant Design 6, TanStack
Query, and Zustand.

```text
src/
  routes/
    definitions.ts       route definition helpers and validation
    registry.ts          feature manifest aggregation and routeMeta derivation
    router.tsx           registry-to-React-Router rendering
    index.tsx            public, portal, and app shells only
  features/
    <domain>/
      routes.ts          domain/feature route manifest
      api.ts             canonical feature wire client when shared by capabilities
      keys.ts            hierarchical query and mutation key factories
      queries.ts         reusable queryOptions factories
      mutations.ts       mutationOptions and invalidation rules
      types.ts           feature contracts and public domain types
      index.ts            narrow public feature surface only
      shared/             proven sibling reuse, never route pages
      <capability>/
        page.tsx          one leaf route when the capability has one route
        list-page.tsx     leaf list route
        detail-page.tsx   leaf detail route
        api.ts            capability wire client when ownership is local
        keys.ts           capability key factory when ownership is local
        queries.ts        capability query options
        mutations.ts      capability mutations and invalidation
        types.ts          capability-only types
        components/       capability-only presentation
        styles.css        capability-owned styles
```

Choose ownership from behavior, not filename symmetry. A small capability does not need every
file. Split a module when it establishes a real boundary, isolates a runtime, removes repeated
logic, or makes tests and ownership clearer.

Use `types.ts` for runtime-adjacent domain types, `api.ts` for HTTP wire calls, semantic page
filenames for leaf routes, and `index.ts` only for a narrow public capability surface.

## 2. Second-Level Capability Boundaries

Split large domains by user-visible workflow and API ownership. Use the current route manifests,
query keys, and wire clients as evidence; do not create a capability folder only to mirror a
menu label.

Promotion rules:

1. Keep code in the owning capability by default.
2. Promote to domain `shared/` only when at least two sibling capabilities have concrete use.
3. Promote to global `src/components`, `src/services`, `src/types`, or `src/utils` only when the
   contract is domain-neutral and broadly reused.
4. Cross-feature consumers must use a feature's public surface or explicit public API/types,
   never a private deep import.
5. `shared/` may contain primitives, contracts, and composition helpers; it must not contain UI
   route pages or become a replacement aggregate feature.

## 3. Route Registry

Route definitions are the single source for path, metadata, shell, permission, menu, redirect,
and loader behavior.

- Define routes with `defineRoutes()` in feature `routes.ts` files.
- Give every route a stable `meta.id` and absolute `meta.path`.
- Define exactly one of `load` or `redirectTo`.
- Use `inheritMetaFrom` for intentional parent metadata inheritance.
- Authenticated UI routes must define permission metadata or a documented exemption.
- Keep aliases and compatibility redirects explicit; preserve required query/context parameters.
- Load real UI routes with a dynamic import of their exact leaf page module.
- A redirect-only group may share a redirect module; unrelated UI pages may not share an
  aggregate loader.
- Do not import page barrels from a route loader.
- Derive `routeMeta` from `registeredRouteDefinitions`; never maintain another metadata table.
- Derive AI `sourceRoute` from React Router location. Explicit resource target routes may remain
  in row actions and AI context payloads.

Run `npm run check:routes` after any path, redirect, permission, menu, shell, metadata, or loader
change. Add route-manifest and deep-link tests near the affected feature.

## 4. Data Layer

### Transport

`src/services/api-client.ts` owns shared request mechanics, authentication headers, envelope
handling, and common errors. Ordinary pages must not import it.

### API

Feature or capability `api.ts` owns endpoint paths, path/query encoding, request payloads, and
wire response normalization. Preserve backend contracts exactly; a folder move is not a reason
to change endpoints or payloads.

### Query Keys

Build hierarchical factories in `keys.ts`:

```ts
export const exampleKeys = {
  all: ['example'] as const,
  lists: ['example', 'list'] as const,
  list: (params: ExampleListParams) => ['example', 'list', normalize(params)] as const,
  details: ['example', 'detail'] as const,
  detail: (id: string) => ['example', 'detail', normalizeId(id)] as const,
}
```

- Normalize whitespace, optional filters, scopes, and IDs before adding them to a key.
- Do not create query keys inline in pages.
- Do not use unrelated bare array keys.
- Make prefix keys available for invalidation.

### Queries

Expose reusable `queryOptions()` factories from `queries.ts`. Put `enabled`, polling, normalized
parameters, and the canonical query function in one place. Pages call `useQuery()` with these
options and own only view-specific composition.

### Mutations

Expose `mutationOptions()` factories from `mutations.ts` and specify exact invalidation in
`onSuccess`. Invalidate the smallest complete set of list/detail/summary prefixes whose data can
change. Test mutation keys, wire inputs, and invalidation behavior.

### Types

- Keep API wire DTOs near the owning API/capability.
- Keep domain types near their feature.
- Keep truly global contracts narrow under `src/types`.
- Do not grow a global catch-all type file to avoid proper ownership.
- Avoid duplicate interfaces that describe the same backend payload.

## 5. State And Context

- TanStack Query owns server state and request lifecycle.
- Existing Zustand stores own persistent auth, platform scope, and user preferences.
- Page-local React state owns selection, active tab, drawer/modal visibility, and transient form
  state.
- URL search parameters own shareable route state.
- Do not introduce a new global store when query state, URL state, or local state is sufficient.
- Preserve permission, workspace, scope mode, theme, and AI context contracts when moving code.

## 6. Pages, Components, And Tests

A route page coordinates route parameters, query/mutation hooks, permission/scope states, and
page-local interaction state. Page composition follows [product-experience.md](product-experience.md);
reusing a query control or AdminTable does not require its old outer page skeleton. Preserve
these coordination boundaries when changing layout. Move reusable presentation and complex pure logic into local
components/hooks/models, but do not hide the entire feature behind another aggregate page.

Co-locate tests with their ownership:

- `api.test.ts`: endpoint, encoding, normalization, and wire behavior;
- `keys.test.ts`: stable normalized key hierarchy;
- `queries.test.ts`: query key/function/enabled/polling contracts;
- `mutations.test.ts`: mutation key, payload, and invalidation;
- `routes.test.ts`: path, metadata, loader, redirect, and compatibility behavior;
- `page.test.tsx`: visible behavior, permission/scope states, actions, loading/empty/error states;
- runtime boundary tests: verify a heavy module loads only after the relevant action or tab.

## 7. CSS And Theme Ownership

- Import capability CSS from its page or capability entry.
- Do not import CSS from API, key, query, mutation, type, model, or utility modules.
- Keep selectors scoped to the capability root class.
- Put shared structural surfaces in existing global/shared component styles.
- Put business-specific states and scene layout in capability CSS.
- Split large domain CSS when independent capabilities can load without it.
- Use Soha tokens and Ant Design semantic APIs; read [theme-system.md](theme-system.md) for
  styling ownership and [product-experience.md](product-experience.md) for product composition.

## 8. Loading And Bundle Boundaries

- One real UI route must resolve to one leaf dynamic source module.
- Keep parent shells and redirect adapters lightweight.
- Dynamically load Flow/VChart, Monaco, xterm, noVNC, terminals, consoles, chart drawers, and
  graph views from the user action or leaf view that needs them.
- Do not statically re-export heavy modules from feature barrels.
- Do not put CSS or heavy runtime imports in shared models or route registries.
- Use manual chunks only for stable third-party runtime grouping, not to compensate for an
  aggregate application module.
- Treat Vite's manifest and `scripts/check-bundle-budget.mjs` as the authoritative size model.

P0 requires no heavy runtime in the initial closure, no circular static chunk group, route
closures below the enforced limits, and page chunks below their limits. P1 is a stricter target;
document evidence for an accepted exception instead of raising thresholds silently.

## 9. Automated Boundaries

`npm run check:frontend-boundaries` must remain at zero for:

- ordinary pages importing `api-client` directly;
- bare query keys;
- cross-feature private deep imports;
- aggregate route modules;
- route loaders importing page barrels;
- shared modules depending on capability route pages.

Do not weaken the checker or rewrite its baseline to conceal new debt.

## 10. Graphify

The Soha Web graph lives in `graphify-out/` relative to the `soha-web` repository.

- Use the existing graph when it helps cross-module analysis; ordinary lookup does not require it.
- Refresh the graph only for broad structural or dependency changes after source edits are
  stable and the worktree contents are understood.
- Follow the Graphify workflow in the repository AGENTS.md after intentional deletions or
  large moves. Verify pruning and diagnose shrink guards; do not use `--force` by default.
- Run graph diagnostics after rebuilding: missing/duplicate node IDs, missing endpoints,
  self-loops, and duplicate/collapsed edges must be visible.
- Audit new high-degree nodes. Shared contracts and explicit coordinators are acceptable;
  multi-route page implementations are not.
- If the graph exceeds the HTML visualization limit, keep `graph.json` and `GRAPH_REPORT.md` as
  the authoritative outputs and report that HTML generation was skipped.

## 11. Completion Checklist

For broad architecture work, verify all of the following:

```bash
npm run format:check
npm run typecheck
npm run lint
npm test
npm run test:coverage
npm run check:routes
npm run check:frontend-boundaries
npm run check:release-workflow
npm run build
npm run check:bundle-budget
antd lint src --format json
git diff --check
```

Use browser regression for canonical routes, redirects with query preservation, direct URL
refresh, permission/module/capability gates, loading/empty/error states, console errors, and
layout overflow. Completion requires code, tests, production manifest, and browser behavior;
broad structural work also requires Graphify evidence. Moving files is not sufficient.
