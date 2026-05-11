# Local Development

## Dependencies

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `kubecrux`
- username: `pgsql`
- password: `pgsql`

## Initialize Local Development Dependencies

```bash
make init
```

This runs `go mod tidy`, installs the `web` and `docs` npm dependencies, then starts PostgreSQL and a local `k3s server` debug cluster and waits until they are ready.
The `k3s` kubeconfig is written to `./.dev/k3s/kubeconfig.yaml`, and the default development config registers it as `local-k3s`.

## Start Backend and Frontend

```bash
make
```

The current frontend local workflow does not depend on a checked-in frontend env template. The default behavior is:

- `web/src/services/api-client.ts` uses same-origin `/api/v1`
- `web/vite.config.ts` proxies `/api` to `http://localhost:8080`
- the docs page is exposed at same-origin `/docs/`, and local Vite development proxies it to `http://localhost:3000/docs/`

Optional shortcuts:

```bash
make init
make dev-api
make dev-web
make dev-docs
```

## Start Docs

```bash
cd docs
npm install
npm run dev
```

## MVP Runtime Notes

The backend bootstraps a local development cluster from `configs/config.yaml` and reads the generated kubeconfig from `./.dev/k3s/kubeconfig.yaml`.

The minimal MVP exposes:

- cluster list
- namespace list
- pod list
- deployment list
- audit write for read APIs
