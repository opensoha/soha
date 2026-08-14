DELETE FROM menu_role_bindings
WHERE menu_id IN (
    'virtualization-workbench-operations',
    'virtualization-workbench-sync',
    'docker-workbench-operations',
    'compute-workbench-tasks-sync',
    'compute-workbench-tasks-build',
    'monitoring-workbench-metrics',
    'monitoring-workbench-traces',
    'monitoring-workbench-logs',
    'monitoring-workbench-alerting'
);

DELETE FROM menus
WHERE id IN (
    'virtualization-workbench-operations',
    'virtualization-workbench-sync',
    'docker-workbench-operations',
    'compute-workbench-tasks-sync',
    'compute-workbench-tasks-build',
    'monitoring-workbench-metrics',
    'monitoring-workbench-traces',
    'monitoring-workbench-logs',
    'monitoring-workbench-alerting'
);

INSERT INTO menus (
    id, parent_id, path, label_zh, label_en, icon_key, section,
    sort_order, enabled, created_at, updated_at
) VALUES
    ('cluster-resources-namespaces', NULL, '/cluster-resources/namespaces', '命名空间', 'Namespaces', 'server', '', 21, true, NOW(), NOW()),
    ('monitoring-workbench-explore', 'monitoring-workbench', '/monitoring-workbench/explore', 'Explore', 'Explore', 'history', 'observe-signals', 63, true, NOW(), NOW()),
    ('ai-workbench-knowledge-pipelines', 'ai-workbench', '/ai-workbench/knowledge-pipelines', 'Knowledge Pipelines', 'Knowledge Pipelines', 'book', 'ai-interaction', 25, true, NOW(), NOW()),
    ('ai-workbench-evaluation-lifecycle', 'ai-workbench', '/ai-workbench/evaluation-lifecycle', 'Evaluation Lifecycle', 'Evaluation Lifecycle', 'inspect', 'ai-interaction', 55, true, NOW(), NOW()),
    ('ai-workbench-memory', 'ai-workbench', '/ai-workbench/memory', 'Memory Policies', 'Memory Policies', 'inspect', 'ai-engineering', 15, true, NOW(), NOW()),
    ('ai-workbench-provider-fleet', 'ai-workbench', '/ai-workbench/provider-fleet', 'Provider Fleet', 'Provider Fleet', 'puzzle', 'ai-engineering', 35, true, NOW(), NOW()),
    ('ai-workbench-environments', 'ai-workbench', '/ai-workbench/environments', 'Agent Environments', 'Agent Environments', 'puzzle', 'ai-engineering', 40, true, NOW(), NOW()),
    ('ai-workbench-production-operations', 'ai-workbench', '/ai-workbench/production-operations', 'AI Operations', 'AI Operations', 'gauge', 'ai-governance', 40, true, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

UPDATE menus AS menu
SET label_zh = labels.label_zh,
    label_en = labels.label_en,
    updated_at = NOW()
FROM (
    VALUES
        ('virtualization-workbench', '/compute/virtualization', '虚拟化资源', 'Virtualization', '虚拟化', 'Virtualization'),
        ('virtualization-workbench-clusters', '/compute/virtualization/clusters', '连接', 'Connections', '集群', 'Clusters'),
        ('virtualization-workbench-images', '/compute/virtualization/images', '镜像', 'Images', '镜像与模板', 'Images & Templates'),
        ('docker-workbench-projects', '/compute/runtimes/projects', '容器项目', 'Container Projects', '容器管理', 'Container Management'),
        ('docker-workbench-templates', '/compute/runtimes/templates', '模板', 'Templates', '部署模板', 'Deployment Templates')
) AS labels(id, path, old_zh, old_en, label_zh, label_en)
WHERE menu.id = labels.id
  AND menu.path = labels.path
  AND menu.label_zh = labels.old_zh
  AND menu.label_en = labels.old_en;
