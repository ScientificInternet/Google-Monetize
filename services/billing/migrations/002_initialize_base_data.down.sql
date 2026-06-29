-- ========================================
-- Google-Monetize 基础数据初始化回滚迁移
-- 回滚价格计划、Token消耗规则和系统配置
-- 迁移ID: 002
-- 版本: v1.0
-- 创建时间: 2025-10-23
-- ========================================

BEGIN;

-- 1. 恢复价格计划到原始状态
UPDATE billing.pricing_plans
SET
    plan_name = 'professional',
    display_name = 'Professional Plan',
    monthly_price_cents = 19900,
    yearly_price_cents = 199000,
    currency = 'USD',
    features = '[]',
    limits = '{}',
    updated_at = now()
WHERE plan_name = 'pro';

UPDATE billing.pricing_plans
SET
    plan_name = 'enterprise',
    display_name = 'Enterprise Plan',
    monthly_price_cents = 99900,
    yearly_price_cents = 999000,
    currency = 'USD',
    features = '[]',
    limits = '{}',
    updated_at = now()
WHERE plan_name = 'elite';

UPDATE billing.pricing_plans
SET
    display_name = 'Starter Plan',
    monthly_price_cents = 4900,
    yearly_price_cents = 49000,
    currency = 'USD',
    features = '[]',
    limits = '{}',
    updated_at = now()
WHERE plan_name = 'starter';

-- 重新启用免费计划
UPDATE billing.pricing_plans
SET is_active = true, updated_at = now()
WHERE plan_name = 'free';

-- 2. 删除Token消耗规则相关
DROP TRIGGER IF EXISTS update_token_consumption_rules_updated_at ON billing.token_consumption_rules;
DROP FUNCTION IF EXISTS billing.update_token_rules_updated_at();
DROP TABLE IF EXISTS billing.token_consumption_rules;

-- 3. 删除系统配置相关
DROP TRIGGER IF EXISTS update_system_configurations_updated_at ON billing.system_configurations;
DROP FUNCTION IF EXISTS billing.update_system_config_updated_at();
DROP TABLE IF EXISTS billing.system_configurations;

-- 4. 删除迁移记录
DELETE FROM public.schema_migrations WHERE version = '002';

COMMIT;

DO $$
BEGIN
    RAISE NOTICE '✅ 基础数据初始化回滚完成';
    RAISE NOTICE '🔄 价格计划已恢复到原始状态';
    RAISE NOTICE '🗑️ Token消耗规则表已删除';
    RAISE NOTICE '🗑️ 系统配置表已删除';
    RAISE NOTICE '🎉 回滚操作成功完成！';
END $$;