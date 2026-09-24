-- 0017: widen environments.status varchar(16) -> varchar(32).
-- AutoMigrate does not alter existing column widths, so the state-machine value
-- 'configured_unverified' (21 chars) failed with SQLSTATE 22001 on every
-- key-field-change rollback (§7.12.6). Idempotent.
ALTER TABLE environments ALTER COLUMN status TYPE varchar(32);
