# Native Linux installation

Use the installation archive from the same release as the server. Keep the raw
binary asset for compatibility; a fresh installation also needs the SQL files.

1. Download `soha-linux-amd64.tar.gz` and `soha-linux-amd64.tar.gz.sha256`.
2. Verify with `sha256sum -c soha-linux-amd64.tar.gz.sha256`.
3. Extract into a **new, empty directory** with `tar -xzf soha-linux-amd64.tar.gz`.
4. Enter `soha/`, copy `configs/config.example.yaml` to `configs/config.yaml`
   only if your private config does not already exist, and configure PostgreSQL.
5. Run `SOHA_CONFIG_FILE=configs/config.yaml ./soha` from that directory.

PostgreSQL must provide the `vector` and `pg_trgm` extensions. The configured
database user must be able to perform the enabled schema migrations. Keep the
entire `migrations/postgres/` directory beside the binary; migrations run before
the HTTP listener starts. Configure the service working directory accordingly.
Check `/healthz`, `/readyz` and `/` after startup.

The example contains public initial credentials and system keys. Replace them
before exposing the service. For an upgrade, back up PostgreSQL, retain your
private configuration, system keys, data directories and previous installation;
extract the new release separately. Never replace an existing credential
encryption key without migrating its stored ciphertext first.

This archive contains Core and its embedded Web console. Optional agents, dsh,
network runtimes and their deployment configuration are installed separately.
