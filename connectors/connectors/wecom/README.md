# WeCom Connector

Status: planned.

This directory is reserved for a future WeCom connector. It is listed in
`connectors/index.json` so layout validation can track the intended public
connector surface.

## Capabilities

| Capability | Direction | Status |
| --- | --- | --- |
| Callback token and encoding AES key verification | Inbound | Planned |
| Message or approval event normalization | Inbound | Planned |
| Bot or app message send action | Outbound | Planned |

## Config

No runtime config is implemented yet. Future config must follow the shared
connector SDK lifecycle and declare fields in a connector manifest before code
lands.

## Limitations

- No source code, manifest, tests, or provider API calls are implemented.
- This connector must not import `soha/internal/...`.
