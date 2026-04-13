# Local Development

## Dependencies

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `kubecrux`
- username: `pgsql`
- password: `pgsql`

### Redis

- host: `localhost`
- port: `6379`
- password: none

## Start Backend

```bash
docker compose up -d postgres redis
go run ./cmd/server
```

## Start Frontend

```bash
cd web
npm install
npm run dev
```

The current frontend local workflow does not depend on a checked-in frontend env template. The default behavior is:

- `web/src/services/api-client.ts` uses same-origin `/api/v1`
- `web/vite.config.ts` proxies `/api` to `http://localhost:8080`
- the docs page embeds the VitePress site at `/docs/`

Optional shortcuts:

```bash
make dev-api
make dev-web
make dev-docs
```

## Start Docs

```bash
cd docs
npm install
npm run docs:dev
```

## MVP Runtime Notes

The backend bootstraps a local cluster entry from `KC_CLUSTER_LOCAL_*` environment variables. By default it reads `~/.kube/config`.

The minimal MVP exposes:

- cluster list
- namespace list
- pod list
- deployment list
- audit write for read APIs
