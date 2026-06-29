-- ========================================
-- Google-Monetize 数据库回滚: Billing Schema
-- 回滚计费域 Schema
-- 迁移ID: 002
-- 版本: v2.0
-- ========================================

-- 开始事务
BEGIN;

-- 删除视图
DROP VIEW IF EXISTS billing.subscription_analytics;
DROP VIEW IF EXISTS billing.token_transaction_summary;
DROP VIEW IF EXISTS billing.user_account_status;

-- 删除触发器
DROP TRIGGER IF EXISTS update_pricing_plans_updated_at ON billing.pricing_plans;
DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON billing.subscriptions;
DROP TRIGGER IF EXISTS update_accounts_updated_at ON billing.accounts;
DROP TRIGGER IF EXISTS update_token_balance_timestamp ON billing.token_balances;

-- 删除函数
DROP FUNCTION IF EXISTS billing.check_subscription_renewals();
DROP FUNCTION IF EXISTS billing.update_token_balance();
DROP FUNCTION IF EXISTS billing.update_updated_at_column();

-- 删除表（按依赖关系逆序）
DROP TABLE IF EXISTS billing.refunds;
DROP TABLE IF EXISTS billing.usage_records;
DROP TABLE IF EXISTS billing.pricing_plans;
DROP TABLE IF EXISTS billing.invoices;
DROP TABLE IF EXISTS billing.subscriptions;
DROP TABLE IF EXISTS billing.token_transactions;
DROP TABLE IF EXISTS billing.token_balances;
DROP TABLE IF EXISTS billing.accounts;

-- 删除Schema
DROP SCHEMA IF EXISTS billing;

-- 提交事务
COMMIT;

-- 验证回滚结果
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'billing') THEN
        RAISE NOTICE '✅ billing schema dropped successfully';
    ELSE
        RAISE EXCEPTION '❌ Failed to drop billing schema';
    END IF;
END $$;

