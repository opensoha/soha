DELETE FROM menu_role_bindings
WHERE menu_id IN (
    'monitoring-workbench-metrics',
    'monitoring-workbench-traces',
    'monitoring-workbench-logs',
    'monitoring-workbench-explore'
);

INSERT INTO menus (
    id, parent_id, path, label_zh, label_en, icon_key, section,
    sort_order, enabled, created_at, updated_at
) VALUES
    ('monitoring-workbench-metrics', 'monitoring-workbench', '/monitoring-workbench/metrics', '指标', 'Metrics', 'activity', 'observe-signals', 63, true, NOW(), NOW()),
    ('monitoring-workbench-traces', 'monitoring-workbench', '/monitoring-workbench/traces', '链路', 'Traces', 'link', 'observe-signals', 64, true, NOW(), NOW()),
    ('monitoring-workbench-logs', 'monitoring-workbench', '/monitoring-workbench/logs', '日志', 'Logs', 'file-clock', 'observe-signals', 65, true, NOW(), NOW())
ON CONFLICT (id) DO UPDATE
SET parent_id = EXCLUDED.parent_id,
    path = EXCLUDED.path,
    label_zh = EXCLUDED.label_zh,
    label_en = EXCLUDED.label_en,
    icon_key = EXCLUDED.icon_key,
    section = EXCLUDED.section,
    sort_order = EXCLUDED.sort_order,
    enabled = EXCLUDED.enabled,
    updated_at = NOW();

UPDATE menus
SET enabled = false,
    updated_at = NOW()
WHERE id = 'monitoring-workbench-explore'
  AND path = '/monitoring-workbench/explore';
