# Local Development

## Dependencies

### PostgreSQL

- host: `localhost`
- port: `5432`
- database: `soha`
- username: `pgsql`
- password: `pgsql`

## Initialize Local Development Dependencies

```bash
make init
```

这会执行 `go mod tidy`、安装 `web` 和 `docs` 的 npm 依赖，然后启动 PostgreSQL 和本地 `k3s server` 调试集群，并等待它们就绪。
`k3s` kubeconfig 会写到 `./.dev/k3s/kubeconfig.yaml`，默认开发配置会把它注册为 `local-k3s`。

## Start Backend and Frontend

```bash
make
```

当前前端本地开发不依赖仓库内的前端 env 模板文件。默认行为是：

- `web/src/services/api-client.ts` 使用同源 `/api/v1`
- `web/vite.config.ts` 把 `/api` 代理到 `http://localhost:8080`
- 文档页通过同源 `/docs/` 访问 Docusaurus 站点，Vite 本地开发会把它代理到 `http://localhost:3000/docs/`

可选快捷命令：

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
