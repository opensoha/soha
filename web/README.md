# Soha Web

This repository owns the Soha web console source. It is a React 18 + Vite +
TypeScript + Ant Design application for the OpenSoha operator console.

The `soha` core repository consumes the built `dist` artifact only. Release
builds copy this artifact into `soha/internal/staticassets/web/dist` before
building the embedded Go binary.

## Repository Layout

- `src/`: application shell, routes, feature pages, shared components, stores,
  services, theme, and i18n.
- `public/`: static browser assets copied by Vite, including `/logo.svg`.
- `.github/workflows/ci.yml`: pull request and main branch verification.
- `.github/workflows/release.yml`: tagged release artifact packaging.
- `@opensoha/contracts`: exact published npm dependency recorded in the lockfile.

## Development

Prerequisites:

- Node.js 22, matching CI.
- npm 10+.
- Registry access to the exact `@opensoha/contracts` version in `package-lock.json`.

```sh
npm ci
npm run dev
```

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

`soha-web` does not edit or publish files inside the core `soha` repository.
The embedding boundary is the built Vite artifact:

```sh
npm ci
npm run build
rm -rf ../soha/internal/staticassets/web/dist
mkdir -p ../soha/internal/staticassets/web
cp -R dist ../soha/internal/staticassets/web/dist
```

After copying, build the `soha` binary from the core repository so the Go
embed/static asset packaging includes the new web console. Release automation
should pin the exact `soha-web` tag or tarball used by a `soha` release, download
the matching `.sha256` file, run `sha256sum -c` before extracting, and fail before
the Go build if checksum verification or `dist/index.html` validation fails.

## License

This repository is licensed under the Apache License 2.0. See
[LICENSE](./LICENSE) for the full license text.
