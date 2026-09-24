-- ============================================================
-- 10_credentials.sql — 演示凭据（接入管理用）
-- 对应页面: 平台管理 → 接入管理 / 组件「环境」Tab 的凭据区
-- 表: credentials
-- 约定: 固定 UUID 主键 + ON CONFLICT (id) DO NOTHING 保证可重复导入。
-- 前缀: b1=credentials
--
-- 注意: 这是**非敏感演示数据**。生产部署前请删除，或经
-- POST /credentials 用真实凭据替换。凭据的明文值由 hub 在写入时按
-- CREDENTIAL_ENCRYPTION_KEY 做 AES-GCM 信封加密（无 key 则明文，dev 模式），
-- 见 internal/credentials/codec。
-- ============================================================

-- 演示用 kubeconfig 占位（值非真实集群，仅用于打通 UI 与「接入」流程）
INSERT INTO credentials (id, name, type, scope, scope_id, value, created_at, updated_at) VALUES
  ('b1000000-0000-0000-0000-000000000001', 'demo-kubeconfig', 'kubeconfig', 'environment', '',
   'demo-only-do-not-use-in-prod: this is a placeholder kubeconfig for local UI smoke',
   now(), now())
ON CONFLICT (id) DO NOTHING;
