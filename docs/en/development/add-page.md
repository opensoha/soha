# Add Page

1. Add or extend the relevant page module under `web/src/features`.
2. Register the route in `web/src/routes/index.tsx`.
3. Add or update route metadata in `web/src/routes/meta.ts` if the page belongs in sidebar navigation or breadcrumbs.
4. Reuse the existing shell, table, modal, and scope-toolbar patterns before introducing a new shared abstraction.
5. Fetch aggregated API data through `web/src/services/api-client.ts` and TanStack Query.
