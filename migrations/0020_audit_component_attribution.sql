-- 0020: audit_log component attribution (ui-12 5.4 组件级审计下沉, 拍板
-- 2026-10-11). The audit middleware denormalizes the owning component onto
-- each mutating row (components direct / pipelines+runs via ComponentLocator);
-- unresolvable rows stay NULL and are invisible to component-scoped queries
-- by design. Idempotent via IF NOT EXISTS.
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS component_id uuid;
CREATE INDEX IF NOT EXISTS idx_audit_log_component ON audit_log(component_id);
