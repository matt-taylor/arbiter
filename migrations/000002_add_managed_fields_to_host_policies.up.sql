ALTER TABLE host_policies ADD COLUMN managed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE host_policies ADD COLUMN managed_pack TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_key TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_version INTEGER NULL;
ALTER TABLE host_policies ADD COLUMN managed_name TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_description TEXT NULL;
ALTER TABLE host_policies ADD COLUMN managed_at TEXT NULL;

CREATE INDEX idx_host_policies_managed_pack ON host_policies(managed_pack);
