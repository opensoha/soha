# Docs Publishing

The documentation site lives at `docs/` and uses Docusaurus.

## Commands

```bash
cd docs
npm install
npm run build
npm run serve
```

Documentation must evolve with code changes. Architecture, API, and development guides are first-class project assets.

The build output lands in `docs/build/`. The Go server embeds that directory through `docs/embed.go` and serves it at same-origin `/docs/`.
