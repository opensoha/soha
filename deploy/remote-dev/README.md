# Remote Kubernetes Development

`ops.popicorns.com` is the shared development endpoint; no separate `dev.ops.popicorns.com` environment is used. The remote development workload reuses the existing `opensoha` namespace PostgreSQL database, `soha-config` Secret, and `soha-data` PVC.

## Start From Pushed Refs

```bash
make remote-dev-up REMOTE_DEV_SOHA_REF=my-branch
```

The workload polls the configured `soha` ref inside the cluster. Backend source changes trigger an automatic rebuild; Vite handles frontend HMR.

## Status, Logs, And Teardown

This workflow adjusts the Helm Deployment replica count and the existing Service selector. Always restore the stable release before running a Helm upgrade:

```bash
make remote-dev-status
make remote-dev-logs
make remote-dev-down
```

> **Note:** This is a shared-data development environment. Schema migrations and application writes directly affect the existing `soha` database.
