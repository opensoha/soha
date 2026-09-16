-- Rename only the built-in labels; preserve administrator customizations.
UPDATE menus SET label_zh = '认证接入', label_en = 'Authentication', updated_at = NOW()
WHERE id = 'identity-providers' AND label_zh = 'Provider' AND label_en = 'Providers';
UPDATE menus SET label_zh = '接入节点', label_en = 'Access Nodes', updated_at = NOW()
WHERE id = 'identity-outposts' AND label_zh = 'Outpost' AND label_en = 'Outposts';
