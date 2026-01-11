DROP INDEX IF EXISTS idx_host_policies_managed_pack;

ALTER TABLE host_policies DROP COLUMN managed_at;
ALTER TABLE host_policies DROP COLUMN managed_description;
ALTER TABLE host_policies DROP COLUMN managed_name;
ALTER TABLE host_policies DROP COLUMN managed_version;
ALTER TABLE host_policies DROP COLUMN managed_key;
ALTER TABLE host_policies DROP COLUMN managed_pack;
ALTER TABLE host_policies DROP COLUMN managed;
