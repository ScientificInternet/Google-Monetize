-- ========================================
-- Google-Monetize 数据库迁移: User Domain Schema
-- Layer 2: 业务用户层
-- 迁移ID: 000001
-- 版本: v2.0
-- 创建时间: 2025-10-22
-- 优先级: 最高 (必须第一个执行，其他服务依赖此表)
-- ========================================

-- 开始事务
BEGIN;

-- ========================================
-- 1. 创建用户域Schema
-- ========================================

CREATE SCHEMA IF NOT EXISTS "user";

-- ========================================
-- 2. 创建用户主表（Layer 2: 业务用户数据）
-- ========================================

-- 数据来源: 从Supabase auth.users同步
-- 职责: 用户基础信息管理、Profile数据
CREATE TABLE "user".users (
    -- 主键: 存储Supabase auth.users.id (UUID转TEXT)
    id TEXT PRIMARY KEY,

    -- 基础信息
    email TEXT UNIQUE NOT NULL,
    name TEXT,
    avatar_url TEXT,
    phone TEXT,

    -- 用户状态
    status TEXT DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'suspended', 'deleted')),

    -- 认证相关
    email_verified BOOLEAN DEFAULT false,
    phone_verified BOOLEAN DEFAULT false,
    last_sign_in_at TIMESTAMPTZ,

    -- 用户偏好设置
    language TEXT DEFAULT 'zh-CN',
    timezone TEXT DEFAULT 'Asia/Shanghai',
    preferences JSONB DEFAULT '{}'::jsonb,

    -- 元数据
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 时间戳
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    deleted_at TIMESTAMPTZ,

    -- 数据完整性约束
    CONSTRAINT users_valid_email CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}$'),
    CONSTRAINT users_valid_language CHECK (language IN ('zh-CN', 'zh-TW', 'en-US', 'en-GB', 'ja-JP', 'ko-KR')),
    CONSTRAINT users_valid_timezone CHECK (timezone IN ('Asia/Shanghai', 'Asia/Tokyo', 'America/New_York', 'America/Los_Angeles', 'Europe/London', 'UTC'))
);

-- ========================================
-- 3. 创建索引
-- ========================================

-- 邮箱索引（唯一约束已自动创建索引）
CREATE INDEX idx_user_users_email ON "user".users(email) WHERE deleted_at IS NULL;

-- 状态索引（只索引活跃用户）
CREATE INDEX idx_user_users_status ON "user".users(status) WHERE status = 'active' AND deleted_at IS NULL;

-- 创建时间索引（用于分页和统计）
CREATE INDEX idx_user_users_created ON "user".users(created_at DESC) WHERE deleted_at IS NULL;

-- 最后登录时间索引（用于活跃度分析）
CREATE INDEX idx_user_users_last_sign_in ON "user".users(last_sign_in_at DESC) WHERE last_sign_in_at IS NOT NULL AND deleted_at IS NULL;

-- 软删除索引
CREATE INDEX idx_user_users_deleted ON "user".users(deleted_at) WHERE deleted_at IS NOT NULL;

-- ========================================
-- 4. 创建触发器和函数
-- ========================================

-- 更新时间戳触发器函数
CREATE OR REPLACE FUNCTION "user".update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 为用户表创建更新时间戳触发器
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON "user".users
    FOR EACH ROW
    EXECUTE FUNCTION "user".update_updated_at_column();

-- ========================================
-- 5. 创建视图
-- ========================================

-- 活跃用户视图（排除软删除和非活跃用户）
CREATE OR REPLACE VIEW "user".active_users AS
SELECT
    id,
    email,
    name,
    avatar_url,
    phone,
    email_verified,
    phone_verified,
    last_sign_in_at,
    language,
    timezone,
    created_at,
    updated_at
FROM "user".users
WHERE status = 'active' AND deleted_at IS NULL;

-- 用户统计视图
CREATE OR REPLACE VIEW "user".user_stats AS
SELECT
    COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_users,
    COUNT(*) FILTER (WHERE status = 'active' AND deleted_at IS NULL) as active_users,
    COUNT(*) FILTER (WHERE status = 'inactive' AND deleted_at IS NULL) as inactive_users,
    COUNT(*) FILTER (WHERE status = 'suspended' AND deleted_at IS NULL) as suspended_users,
    COUNT(*) FILTER (WHERE email_verified = true AND deleted_at IS NULL) as verified_users,
    COUNT(*) FILTER (WHERE last_sign_in_at >= now() - INTERVAL '30 days' AND deleted_at IS NULL) as active_last_30_days,
    COUNT(*) FILTER (WHERE created_at >= now() - INTERVAL '7 days' AND deleted_at IS NULL) as new_users_last_7_days
FROM "user".users;

-- ========================================
-- 6. 表注释
-- ========================================

COMMENT ON SCHEMA "user" IS 'Layer 2: 业务用户域 - 管理所有业务用户的基础信息和偏好设置';
COMMENT ON TABLE "user".users IS 'Layer 2核心表: 业务用户主表，数据源自Supabase auth.users，所有业务域通过user_id引用此表';
COMMENT ON COLUMN "user".users.id IS '主键: Supabase auth.users.id (UUID转TEXT)';
COMMENT ON COLUMN "user".users.email IS '用户邮箱: 唯一标识，来自Supabase';
COMMENT ON COLUMN "user".users.status IS '用户状态: active(活跃)/inactive(未激活)/suspended(停用)/deleted(软删除)';
COMMENT ON COLUMN "user".users.preferences IS 'JSONB: 用户偏好设置，如通知偏好、显示设置等';
COMMENT ON COLUMN "user".users.metadata IS 'JSONB: 扩展元数据，用于存储额外的业务数据';

-- ========================================
-- 7. 提交事务
-- ========================================

COMMIT;

-- ========================================
-- 8. 验证Schema创建
-- ========================================

DO $$
BEGIN
    -- 验证schema
    IF EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'user') THEN
        RAISE NOTICE '✅ user schema created successfully';
    ELSE
        RAISE EXCEPTION '❌ Failed to create user schema';
    END IF;

    -- 验证表
    IF EXISTS (SELECT 1 FROM information_schema.tables
               WHERE table_schema = 'user' AND table_name = 'users') THEN
        RAISE NOTICE '✅ user.users table created successfully';
    ELSE
        RAISE EXCEPTION '❌ Failed to create user.users table';
    END IF;

    -- 验证索引
    IF (SELECT COUNT(*) FROM pg_indexes
        WHERE schemaname = 'user' AND tablename = 'users') >= 5 THEN
        RAISE NOTICE '✅ user.users indexes created successfully';
    ELSE
        RAISE WARNING '⚠️  Expected at least 5 indexes on user.users';
    END IF;

    -- 验证触发器
    IF EXISTS (SELECT 1 FROM information_schema.triggers
               WHERE trigger_schema = 'user' AND trigger_name = 'update_users_updated_at') THEN
        RAISE NOTICE '✅ user.users triggers created successfully';
    ELSE
        RAISE WARNING '⚠️  Expected update trigger on user.users';
    END IF;

    RAISE NOTICE '';
    RAISE NOTICE '🎉 User Domain (Layer 2) 初始化完成!';
    RAISE NOTICE '📊 核心特性:';
    RAISE NOTICE '   ✅ Layer 2核心表: user.users';
    RAISE NOTICE '   ✅ 软删除支持';
    RAISE NOTICE '   ✅ 邮箱验证和格式校验';
    RAISE NOTICE '   ✅ 用户状态管理';
    RAISE NOTICE '   ✅ 多语言和时区支持';
    RAISE NOTICE '   ✅ JSONB扩展字段';
    RAISE NOTICE '   ✅ 性能优化索引';
    RAISE NOTICE '   ✅ 自动更新时间戳';
    RAISE NOTICE '';
    RAISE NOTICE '⚠️  重要提示:';
    RAISE NOTICE '   - 此表必须第一个创建（其他服务依赖）';
    RAISE NOTICE '   - id字段存储Supabase auth.users.id';
    RAISE NOTICE '   - 所有业务域通过user_id TEXT引用此表';
    RAISE NOTICE '';
END $$;
