-- Organization names are labels within a hierarchy, not global identifiers.
-- Different parents may legitimately contain children with the same name.
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_name_key;
