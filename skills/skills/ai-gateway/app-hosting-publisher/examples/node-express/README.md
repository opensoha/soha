# Node + TypeScript + Express starter

Required root files: `package.json`, `package-lock.json`, checked-in `server.js`, and `soha-host.json`. `server.ts` is the editable source; `npm run build` refreshes `server.js`.

```sh
cp -R skills/skills/ai-gateway/app-hosting-publisher/examples/node-express ./my-node-app
cd my-node-app
npm ci
npm run build
soha host deploy . --kind dynamic --name "My Node app" --dry-run --output json
soha host deploy . --kind dynamic --name "My Node app" --yes --output json
```

The app listens on `PORT` (default `8080`) and serves `GET /healthz`. `DATABASE_URL` is injected by App Hosting and scoped to `app.entries`. Identity comes from the gateway headers `X-Soha-User-ID` and `X-Soha-User` (the actual display-name header in `internal/appgateway/dynamic.go`).

`GET /api/entries` lists the caller's saved entries and `POST /api/entries` upserts `{"key":"example","value":{}}`. Use `npm start` for a local run after setting `DATABASE_URL`; never commit `.env` files or credentials.
