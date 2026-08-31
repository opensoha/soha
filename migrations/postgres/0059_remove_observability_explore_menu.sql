DELETE FROM menu_role_bindings
WHERE menu_id = 'monitoring-workbench-explore';

DELETE FROM menus
WHERE id = 'monitoring-workbench-explore';
