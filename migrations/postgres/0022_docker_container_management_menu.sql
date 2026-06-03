UPDATE menus
SET label_zh = '容器管理',
    label_en = 'Container Management',
    updated_at = NOW()
WHERE id = 'docker-workbench-projects'
  AND label_zh = 'Compose 项目'
  AND label_en = 'Compose Projects';

DELETE FROM menu_role_bindings
WHERE menu_id IN ('docker-workbench-services', 'docker-workbench-ports');

DELETE FROM menus
WHERE id IN ('docker-workbench-services', 'docker-workbench-ports');
