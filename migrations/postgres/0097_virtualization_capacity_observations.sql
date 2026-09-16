-- Reject an older concurrent provider read after a newer observation has
-- accounted a reservation. Otherwise stale free capacity could be reused.
CREATE TABLE IF NOT EXISTS virtualization_capacity_observations (
    source_id TEXT PRIMARY KEY,
    observed_at TIMESTAMPTZ NOT NULL
);
