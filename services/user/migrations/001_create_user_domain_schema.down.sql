-- ========================================
-- Google-Monetize 数据库迁移回滚: User Schema
-- 删除用户域Schema
-- 迁移ID: 000001
-- ========================================

-- 开始事务
BEGIN;

-- 删除用户表
DROP TABLE IF EXISTS "user".users CASCADE;

-- 删除Schema
DROP SCHEMA IF EXISTS "user" CASCADE;

-- 提交事务
COMMIT;

-- 验证回滚结果
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'user') THEN
        RAISE NOTICE '✅ user schema dropped successfully';
    ELSE
        RAISE EXCEPTION '❌ Failed to drop user schema';
    END IF;
END $$;
