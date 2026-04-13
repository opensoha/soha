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

当前前端本地开发不依赖仓库内的前端 env 模板文件。默认行为是：

- `web/src/services/api-client.ts` 使用同源 `/api/v1`
- `web/vite.config.ts` 把 `/api` 代理到 `http://localhost:8080`
- 文档页通过 `/docs/` 内嵌 VitePress 站点

可选快捷命令：

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
