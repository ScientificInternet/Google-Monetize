-- ========================================
-- Google-Monetize 数据库迁移: Billing Schema
-- 创建计费域 (Billing Domain) Schema
-- 迁移ID: 002
-- 版本: v2.0
-- 创建时间: 2025-10-21
-- ========================================

-- 开始事务
BEGIN;

-- 创建计费域Schema
CREATE SCHEMA IF NOT EXISTS billing;

-- 设置Schema权限

-- 1. 用户计费账户
CREATE TABLE billing.accounts (
    user_id TEXT PRIMARY KEY REFERENCES "user".users(id) ON DELETE CASCADE,
    account_type TEXT DEFAULT 'standard' CHECK (account_type IN ('standard', 'premium', 'enterprise')),
    status TEXT DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'cancelled', 'trial')),
    balance_cents BIGINT DEFAULT 0 CHECK (balance_cents >= 0),
    currency TEXT DEFAULT 'USD' CHECK (currency ~* '^[A-Z]{3}$'),
    credit_limit_cents BIGINT DEFAULT 0 CHECK (credit_limit_cents >= 0),
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    billing_address JSONB DEFAULT '{}'::jsonb,
    payment_methods JSONB DEFAULT '[]'::jsonb,

    -- 数据完整性约束
    CONSTRAINT accounts_valid_balance CHECK (balance_cents <= 100000000), -- $1,000,000 max
    CONSTRAINT accounts_valid_credit CHECK (credit_limit_cents <= 10000000) -- $100,000 max
);

-- 2. 代币余额表
CREATE TABLE billing.token_balances (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    token_type TEXT NOT NULL DEFAULT 'search' CHECK (token_type IN ('search', 'analysis', 'export', 'bulk_operation')),
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    last_updated TIMESTAMPTZ DEFAULT now(),
    updated_by TEXT REFERENCES "user".users(id),
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 唯一约束
    UNIQUE(user_id, token_type)
);

-- 3. 代币交易记录
CREATE TABLE billing.token_transactions (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    token_type TEXT NOT NULL,
    amount BIGINT NOT NULL, -- 正数为充值，负数为消费
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    transaction_type TEXT NOT NULL CHECK (transaction_type IN ('purchase', 'consumption', 'refund', 'adjustment', 'bonus', 'penalty')),
    source TEXT NOT NULL CHECK (source IN ('system', 'user', 'admin', 'api', 'automatic')),
    description TEXT,
    reference_id TEXT, -- 关联的业务ID，如offer_id, analysis_id等
    created_at TIMESTAMPTZ DEFAULT now(),
    created_by TEXT REFERENCES "user".users(id),
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 数据完整性约束
    CONSTRAINT transactions_balance_consistency CHECK (balance_after = balance_before + amount),
    CONSTRAINT transactions_reasonable_amount CHECK (ABS(amount) <= 100000), -- $1,000 max per transaction
    CONSTRAINT transactions_not_zero_amount CHECK (amount != 0)
);

-- 4. 订阅管理
CREATE TABLE billing.subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    plan_name TEXT NOT NULL CHECK (plan_name IN ('free', 'starter', 'professional', 'enterprise')),
    status TEXT NOT NULL CHECK (status IN ('active', 'cancelled', 'expired', 'paused', 'trial')),
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    trial_end TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency TEXT DEFAULT 'USD' CHECK (currency ~* '^[A-Z]{3}$'),
    billing_interval TEXT NOT NULL CHECK (billing_interval IN ('month', 'year')),
    auto_renew BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 数据完整性约束
    CONSTRAINT subscriptions_period_consistency CHECK (current_period_end > current_period_start),
    CONSTRAINT subscriptions_trial_future CHECK (trial_end IS NULL OR trial_end > created_at)
);

-- 5. 发票记录
CREATE TABLE billing.invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    invoice_number TEXT UNIQUE NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'sent', 'paid', 'void', 'refunded', 'overdue')),
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    currency TEXT DEFAULT 'USD' CHECK (currency ~* '^[A-Z]{3}$'),
    due_date TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    line_items JSONB DEFAULT '[]'::jsonb,
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 数据完整性约束
    CONSTRAINT invoices_amount_positive CHECK (amount_cents > 0),
    CONSTRAINT invoices_due_future CHECK (due_date IS NULL OR due_date >= created_at)
);

-- 6. 价格计划配置
CREATE TABLE billing.pricing_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_name TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT,
    monthly_price_cents INTEGER NOT NULL CHECK (monthly_price_cents >= 0),
    yearly_price_cents INTEGER NOT NULL CHECK (yearly_price_cents >= 0),
    currency TEXT DEFAULT 'USD',
    features JSONB DEFAULT '[]'::jsonb,
    limits JSONB DEFAULT '{}'::jsonb,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- 7. 使用记录 (用于跟踪代币使用情况)
CREATE TABLE billing.usage_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    usage_type TEXT NOT NULL CHECK (usage_type IN ('search', 'analysis', 'export', 'api_call', 'bulk_operation')),
    resource_id TEXT, -- 被使用的资源ID
    resource_type TEXT, -- offer, analysis, export_file等
    tokens_consumed INTEGER NOT NULL DEFAULT 0 CHECK (tokens_consumed >= 0),
    cost_cents INTEGER DEFAULT 0 CHECK (cost_cents >= 0),
    created_at TIMESTAMPTZ DEFAULT now(),
    metadata JSONB DEFAULT '{}'::jsonb
);

-- 8. 退款记录
CREATE TABLE billing.refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL REFERENCES billing.accounts(user_id) ON DELETE CASCADE,
    transaction_id BIGINT REFERENCES billing.token_transactions(id),
    refund_amount_cents INTEGER NOT NULL CHECK (refund_amount_cents > 0),
    refund_reason TEXT NOT NULL,
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'processed')),
    processed_by TEXT REFERENCES "user".users(id),
    approved_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    notes TEXT,
    metadata JSONB DEFAULT '{}'::jsonb
);

-- ========================================
-- 创建索引
-- ========================================

-- 账户表索引
CREATE INDEX idx_billing_accounts_status ON billing.accounts(status);
CREATE INDEX idx_billing_accounts_type ON billing.accounts(account_type);
CREATE INDEX idx_billing_accounts_balance ON billing.accounts(balance_cents) WHERE balance_cents > 0;

-- 代币余额表索引
CREATE INDEX idx_billing_token_balances_user_type ON billing.token_balances(user_id, token_type);
CREATE INDEX idx_billing_token_balances_balance ON billing.token_balances(balance) WHERE balance > 0;

-- 交易记录表索引
CREATE INDEX idx_billing_transactions_user_created ON billing.token_transactions(user_id, created_at DESC);
CREATE INDEX idx_billing_transactions_type_created ON billing.token_transactions(transaction_type, created_at DESC);
CREATE INDEX idx_billing_transactions_reference ON billing.token_transactions(reference_id) WHERE reference_id IS NOT NULL;

-- 订阅表索引
CREATE INDEX idx_billing_subscriptions_user_status ON billing.subscriptions(user_id, status);
CREATE INDEX idx_billing_subscriptions_period_end ON billing.subscriptions(current_period_end);
CREATE INDEX idx_billing_subscriptions_plan ON billing.subscriptions(plan_name);

-- 发票表索引
CREATE INDEX idx_billing_invoices_user ON billing.invoices(user_id);
CREATE INDEX idx_billing_invoices_status ON billing.invoices(status);
CREATE INDEX idx_billing_invoices_due_date ON billing.invoices(due_date) WHERE due_date IS NOT NULL;

-- 使用记录索引
CREATE INDEX idx_billing_usage_records_user_type ON billing.usage_records(user_id, usage_type);
CREATE INDEX idx_billing_usage_records_created ON billing.usage_records(created_at DESC);

-- 退款记录索引
CREATE INDEX idx_billing_refunds_user_status ON billing.refunds(user_id, status);
CREATE INDEX idx_billing_refunds_transaction ON billing.refunds(transaction_id);

-- ========================================
-- 创建触发器和函数
-- ========================================

-- 更新时间戳触发器函数
CREATE OR REPLACE FUNCTION billing.update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 为账户表创建更新时间戳触发器
CREATE TRIGGER update_accounts_updated_at
    BEFORE UPDATE ON billing.accounts
    FOR EACH ROW
    EXECUTE FUNCTION billing.update_updated_at_column();

-- 为订阅表创建更新时间戳触发器
CREATE TRIGGER update_subscriptions_updated_at
    BEFORE UPDATE ON billing.subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION billing.update_updated_at_column();

-- 为价格计划表创建更新时间戳触发器
CREATE TRIGGER update_pricing_plans_updated_at
    BEFORE UPDATE ON billing.pricing_plans
    FOR EACH ROW
    EXECUTE FUNCTION billing.update_updated_at_column();

-- 代币余额更新触发器
CREATE OR REPLACE FUNCTION billing.update_token_balance()
RETURNS TRIGGER AS $$
BEGIN
    -- 更新余额的最后更新时间
    NEW.last_updated = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_token_balance_timestamp
    BEFORE UPDATE ON billing.token_balances
    FOR EACH ROW
    EXECUTE FUNCTION billing.update_token_balance();

-- 订阅续费检查函数
CREATE OR REPLACE FUNCTION billing.check_subscription_renewals()
RETURNS void AS $$
BEGIN
    -- 标记过期的订阅
    UPDATE billing.subscriptions
    SET status = 'expired'
    WHERE status = 'active'
    AND current_period_end <= now()
    AND auto_renew = false;

    -- 处理自动续费
    UPDATE billing.subscriptions
    SET
        status = 'active',
        current_period_start = current_period_end,
        current_period_end = current_period_end + CASE
            WHEN billing_interval = 'month' THEN INTERVAL '1 month'
            WHEN billing_interval = 'year' THEN INTERVAL '1 year'
            ELSE INTERVAL '1 month'
        END
    WHERE status = 'active'
    AND current_period_end <= now()
    AND auto_renew = true
    AND trial_end IS NOT NULL AND trial_end <= now();
END;
$$ LANGUAGE plpgsql;

-- ========================================
-- 创建视图
-- ========================================

-- 用户账户状态视图
CREATE OR REPLACE VIEW billing.user_account_status AS
SELECT
    a.user_id,
    a.account_type,
    a.status as account_status,
    a.balance_cents,
    a.currency,
    u.email,
    u.name,
    s.status as subscription_status,
    s.plan_name,
    s.current_period_end,
    s.auto_renew,
    a.created_at,
    a.updated_at,
    COALESCE(tb.total_tokens, 0) as total_tokens
FROM billing.accounts a
LEFT JOIN "user".users u ON a.user_id = u.id
LEFT JOIN billing.subscriptions s ON a.user_id = s.user_id AND s.status = 'active'
LEFT JOIN (
    SELECT user_id, SUM(balance) as total_tokens
    FROM billing.token_balances
    GROUP BY user_id
) tb ON a.user_id = tb.user_id;

-- 代币交易汇总视图
CREATE OR REPLACE VIEW billing.token_transaction_summary AS
SELECT
    user_id,
    token_type,
    COUNT(*) as total_transactions,
    SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END) as total_earned,
    ABS(SUM(CASE WHEN amount < 0 THEN amount ELSE 0 END)) as total_spent,
    SUM(amount) as net_change,
    MAX(created_at) as last_transaction,
    MIN(created_at) as first_transaction
FROM billing.token_transactions
GROUP BY user_id, token_type;

-- 订阅统计视图
CREATE OR REPLACE VIEW billing.subscription_analytics AS
SELECT
    plan_name,
    status,
    COUNT(*) as subscription_count,
    SUM(amount_cents) as total_revenue_cents,
    AVG(amount_cents) as avg_revenue_cents,
    COUNT(CASE WHEN billing_interval = 'month' THEN 1 END) as monthly_subscriptions,
    COUNT(CASE WHEN billing_interval = 'year' THEN 1 END) as yearly_subscriptions,
    COUNT(CASE WHEN auto_renew = true THEN 1 END) as auto_renewing_subscriptions,
    DATE_TRUNC('month', created_at) as month
FROM billing.subscriptions
GROUP BY plan_name, status, DATE_TRUNC('month', created_at);

-- ========================================
-- 初始化数据
-- ========================================

-- 插入默认价格计划
INSERT INTO billing.pricing_plans (plan_name, display_name, description, monthly_price_cents, yearly_price_cents, features, limits, is_active)
VALUES
    ('free', 'Free Plan', 'Basic features for individual users', 0, 0,
     '["100 search tokens/month", "Basic support", "Standard analytics"]',
     '{"search_tokens": 100, "analysis_tokens": 0, "export_tokens": 0}', true),
    ('starter', 'Starter Plan', 'Perfect for small businesses', 4900, 49000,
     '["1000 search tokens/month", "100 analysis tokens/month", "Email support", "Advanced analytics", "API access"]',
     '{"search_tokens": 1000, "analysis_tokens": 100, "export_tokens": 100, "api_calls_per_day": 1000}', true),
    ('professional', 'Professional Plan', 'For growing businesses', 19900, 199000,
     '["5000 search tokens/month", "1000 analysis tokens/month", "Priority support", "Custom analytics", "Full API access", "Team collaboration"]',
     '{"search_tokens": 5000, "analysis_tokens": 1000, "export_tokens": 500, "api_calls_per_day": 10000, "team_members": 5}', true),
    ('enterprise', 'Enterprise Plan', 'For large organizations', 99900, 999000,
     '["Unlimited search tokens", "Unlimited analysis tokens", "Dedicated support", "Custom features", "Full API access", "Unlimited team members", "SLA guarantee"]',
     '{"search_tokens": -1, "analysis_tokens": -1, "export_tokens": -1, "api_calls_per_day": -1, "team_members": -1}', true)
ON CONFLICT (plan_name) DO NOTHING;

-- ========================================
-- 提交事务
-- ========================================

COMMIT;

-- ========================================
-- 验证脚本执行结果
-- ========================================

-- 验证Schema创建
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'billing') THEN
        RAISE NOTICE '✅ billing schema created successfully';
    ELSE
        RAISE EXCEPTION '❌ Failed to create billing schema';
    END IF;
END $$;