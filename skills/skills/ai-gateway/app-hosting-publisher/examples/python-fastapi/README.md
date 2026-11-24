# Python + FastAPI starter

Required root files: `app.py`, `requirements.txt`, and `soha-host.json`. Every direct and transitive package is pinned because the hosted build installs with `--only-binary=:all: --no-deps`.

```sh
cp -R skills/skills/ai-gateway/app-hosting-publisher/examples/python-fastapi ./my-python-app
cd my-python-app
python3 -m pip install -r requirements.txt
soha host deploy . --kind dynamic --name "My Python app" --dry-run --output json
soha host deploy . --kind dynamic --name "My Python app" --yes --output json
```

The app listens on `PORT` (default `8080`) and serves `GET /healthz`. `DATABASE_URL` is injected by App Hosting and scoped to `app.entries`. Identity comes from the gateway headers `X-Soha-User-ID` and `X-Soha-User` (the actual display-name header in `internal/appgateway/dynamic.go`).

`GET /api/entries` lists the caller's saved entries and `POST /api/entries` upserts `{"key":"example","value":{}}`. Use `python3 app.py` for a local run after setting `DATABASE_URL`; never commit `.env` files or credentials.
