-- =====================================================================
-- 接入 Keycloak:users 表关联 Keycloak 的 sub 声明
-- =====================================================================
alter table users add column keycloak_id varchar(64);
create unique index idx_users_keycloak_id on users(keycloak_id) where keycloak_id is not null;
