# Soha Web

This `web/` workspace owns the Soha web console source within the main repository. It is a React 18 + Vite +
TypeScript + Ant Design application for the OpenSoha operator console.

Core remains at the repository root and consumes the built `web/dist` artifact only.
The root build stages it into `internal/staticassets/web/dist` before building the embedded Go binary.

## Repository Layout

- `src/`: application shell, routes, feature pages, shared components, stores,
  services, theme, and i18n.
- `public/`: static browser assets copied by Vite, including `/logo.svg`.
- `../.github/workflows/`: executable root CI and artifact workflows; nested historical workflows are not GitHub entry points.
- `@opensoha/contracts`: same-checkout `../contracts` source workspace; the root lockfile owns installation.

## Development

Prerequisites:

- Node.js 22, matching CI.
- npm 10+.
- Registry access for the dependencies in the root `../package-lock.json`; the SDK itself resolves to local `contracts/` source.

From the monorepo root (the parent of `web/`):

```sh
npm ci
npm run check:workspace
npm run dev --workspace web
```

Use only the root lockfile. Do not install or create locks in `web/` or `contracts/`.
Other component scripts can still run from `web/`, or with `npm run <script> --workspace web`
from the root. Workspace source verification does not prove that the SDK is published.

By default the Vite dev server starts on `http://localhost:5173`.

### Environment Variables

| Variable            | Default   | Purpose                                                                                                                      |
| ------------------- | --------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `VITE_API_BASE_URL` | `/api/v1` | Browser API base URL used by auth and console requests. Use an absolute URL when the API is not served from the same origin. |

Local development can rely on the Vite proxy in `vite.config.ts`:

- `/api` -> `http://127.0.0.1:8080`
- `/docs` -> `http://localhost:3000`

With the default `VITE_API_BASE_URL=/api/v1`, frontend requests are proxied to
`http://127.0.0.1:8080/api/v1`.

### Auth Flow

The console stores only the user profile in persisted browser state. Access
tokens are kept in memory and restored by calling `/auth/refresh` with the
server-side refresh cookie. Protected routes use `AuthGuard`, then load the
permission snapshot from `/access/permission-snapshot`.

Global API handling covers:

- `401`: attempt refresh once, clear local auth when refresh fails, notify the
  user, and redirect to `/login`.
- `403`: show a permission warning with request context.
- `5xx` and network failures: show an operator-facing error notification with
  request ID when the backend provides one.

## Build

```sh
npm run typecheck
npm test
npm run test:coverage
npm run lint
npm run format:check
npm run build
```

`npm run build` runs TypeScript checking before Vite emits `dist`.

## Quality Gates

CI and release workflows run:

- `npm run lint`
- `npm run format:check`
- `npm run typecheck`
- `npm test`
- `npm run test:coverage`
- `npm run build`

Coverage uses Vitest with the V8 provider and repository thresholds configured
in `vite.config.ts`.

The current format gate intentionally covers repository configuration, docs,
entrypoints, and the shared service/component files touched by the gate. This
keeps the new check enforceable without reformatting older generated or large
feature files in unrelated changes.

## Release Artifact

The standalone distribution procedure below describes the legacy published-artifact chain.
B2 source-workspace builds use the root commands above; publisher migration is a separate release stage.

Tagged releases and manual workflow dispatches run `.github/workflows/release.yml`.
The workflow:

1. Checks out `soha-web`.
2. Verifies the exact reviewed `@opensoha/contracts` version and registry
   integrity in the lockfile, then installs with `npm ci`. An optional manual
   version assertion must match the lockfile; it never rewrites dependencies.
3. Runs lint, format, tests, coverage, and build.
4. Packages `dist` as `soha-web-dist-${GITHUB_REF_NAME}.tar.gz`.
5. Writes `soha-web-dist-${GITHUB_REF_NAME}.tar.gz.sha256` and verifies it with
   `sha256sum -c`.
6. Runs `npm run release:verify-dist -- --artifact <tarball>` to validate the
   tarball checksum, `index.html`, built assets, and safe tar members.
7. Uploads the tarball and checksum as workflow artifacts and, for `v*` tags, as
   GitHub release assets.
8. Downloads the published GitHub release assets and runs the same dist artifact
   verification against the downloaded tarball.

## Embedding Into `soha`

Web source is in `web/`; Core consumes its built `dist` artifact. From the monorepo root:

```sh
npm run build:web
```

This generates the same-checkout SDK, builds Web and stages the verified artifact into
`internal/staticassets/web/dist` through `scripts/source-workspace.py stage-web`.
The helper records file hashes and modes; build the Core binary only after staging succeeds.
Do not copy an old sibling checkout's `dist` or an old Web release into this source build.
The public SDK module and package identities remain unchanged during the B2 transition.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
