UPDATE menus
SET label_zh = '总览',
    label_en = 'Overview',
    updated_at = NOW()
WHERE id = 'monitoring-workbench-overview'
  AND path = '/monitoring-workbench/overview';
