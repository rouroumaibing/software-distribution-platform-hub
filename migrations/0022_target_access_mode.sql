-- 0022: targets 扩 access_mode + credential_ref（O-40，F2.1 统一纳管面）。
-- access_mode 默认 agent（兼容既有 agent-only 反向连接通道）；kubeconfig/ssh 经
-- credential_ref 引用上游凭据（凭据存在的核验在上游凭据服务落地，本列只存引用）。
-- agent 通道对 access_mode 透明：agent_op.go 不读该字段。幂等。
-- 升级前备份：pg_dump -t targets <db> > /tmp/targets_pre0022.sql
BEGIN;
ALTER TABLE targets ADD COLUMN IF NOT EXISTS access_mode varchar(16) NOT NULL DEFAULT 'agent';
ALTER TABLE targets ADD COLUMN IF NOT EXISTS credential_ref varchar(255);
COMMIT;
