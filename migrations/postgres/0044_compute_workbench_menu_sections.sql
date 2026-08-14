UPDATE menus AS menu
SET section = '', updated_at = NOW()
FROM (
    VALUES
        ('compute-workbench', '/compute'),
        ('compute-workbench-overview', '/compute/overview'),
        ('compute-workbench-tasks-operations', '/compute/tasks/operations')
) AS builtin(id, path)
WHERE menu.id = builtin.id
  AND menu.path = builtin.path
  AND menu.section = 'ops';
