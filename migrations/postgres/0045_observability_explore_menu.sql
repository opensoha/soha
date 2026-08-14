UPDATE menus AS menu
SET enabled = false, updated_at = NOW()
FROM (
    VALUES
        ('monitoring-workbench-metrics', '/monitoring-workbench/metrics'),
        ('monitoring-workbench-traces', '/monitoring-workbench/traces'),
        ('monitoring-workbench-logs', '/monitoring-workbench/logs')
) AS builtin(id, path)
WHERE menu.id = builtin.id
  AND menu.path = builtin.path;
