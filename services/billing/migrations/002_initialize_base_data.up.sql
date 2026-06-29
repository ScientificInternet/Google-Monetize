-- ========================================
-- Google-Monetize 基础数据初始化迁移
-- 初始化价格计划、Token消耗规则和系统配置
-- 迁移ID: 002
-- 版本: v1.0
-- 创建时间: 2025-10-23
-- ========================================

BEGIN;

-- 1. 更新现有价格计划
UPDATE billing.pricing_plans
SET
    display_name = 'Starter',
    description = '适合个人和小型项目的基础套餐',
    monthly_price_cents = 29800,  -- ¥298/月
    yearly_price_cents = 178800,  -- ¥298×12×0.5 = ¥1788/年
    currency = 'CNY',
    features = '["ai_analysis", "basic_seo", "export_data"]',
    limits = JSONB_BUILD_OBJECT(
        'token_allowance_monthly', 100,
        'max_projects', 3,
        'max_domains_per_project', 5,
        'competitor_monitoring', false,
        'batch_analysis', false,
        'custom_reports', false,
        'api_access', false,
        'priority_support', false
    ),
    updated_at = now()
WHERE plan_name = 'starter';

UPDATE billing.pricing_plans
SET
    plan_name = 'pro',
    display_name = 'Pro',
    description = '适合专业团队和成长型企业的标准套餐',
    monthly_price_cents = 99800,   -- ¥998/月
    yearly_price_cents = 598800,   -- ¥998×12×0.5 = ¥5988/年
    currency = 'CNY',
    features = '["ai_analysis", "basic_seo", "competitor_monitoring", "batch_analysis", "custom_reports", "api_access", "priority_support", "export_data"]',
    limits = JSONB_BUILD_OBJECT(
        'token_allowance_monthly', 1000,
        'max_projects', 10,
        'max_domains_per_project', 20,
        'competitor_monitoring', true,
        'batch_analysis', true,
        'custom_reports', true,
        'api_access', true,
        'priority_support', true
    ),
    updated_at = now()
WHERE plan_name = 'professional';

UPDATE billing.pricing_plans
SET
    plan_name = 'elite',
    display_name = 'Elite',
    description = '适合大型企业和高级用户的完整套餐',
    monthly_price_cents = 299800,  -- ¥2,998/月
    yearly_price_cents = 1798800, -- ¥2,998×12×0.5 = ¥17,988/年
    currency = 'CNY',
    features = '["ai_analysis", "basic_seo", "competitor_monitoring", "batch_analysis", "custom_reports", "api_access", "priority_support", "export_data", "custom_ai_models", "white_label", "dedicated_support"]',
    limits = JSONB_BUILD_OBJECT(
        'token_allowance_monthly', 10000,
        'max_projects', -1,
        'max_domains_per_project', -1,
        'custom_ai_models', true,
        'white_label', true,
        'dedicated_support', true
    ),
    updated_at = now()
WHERE plan_name = 'enterprise';

-- 禁用免费计划
UPDATE billing.pricing_plans
SET is_active = false, updated_at = now()
WHERE plan_name = 'free';

-- 2. 创建Token消耗规则表
CREATE TABLE IF NOT EXISTS billing.token_consumption_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feature_key TEXT UNIQUE NOT NULL,
    feature_name TEXT NOT NULL,
    operation_type TEXT NOT NULL, -- 'per_request', 'per_page', 'per_keyword', 'per_domain', etc.
    base_cost INTEGER NOT NULL, -- 基础Token消耗
    cost_unit TEXT NOT NULL, -- 消耗单位: 'tokens', 'tokens_per_page', 'tokens_per_keyword', etc.
    pricing_plan_limits JSONB DEFAULT '{}'::jsonb, -- 不同套餐的限制
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- 创建自动更新时间戳的函数
CREATE OR REPLACE FUNCTION billing.update_token_rules_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 创建触发器
CREATE TRIGGER update_token_consumption_rules_updated_at
    BEFORE UPDATE ON billing.token_consumption_rules
    FOR EACH ROW EXECUTE FUNCTION billing.update_token_rules_updated_at();

-- 3. 插入Token消耗规则数据
INSERT INTO billing.token_consumption_rules (
    feature_key, feature_name, operation_type, base_cost, cost_unit, pricing_plan_limits, is_active
) VALUES
-- AI分析相关
('ai_page_analysis', 'AI页面分析', 'per_page', 100, 'tokens_per_page',
 '{"starter": {"daily_limit": 10, "monthly_limit": 100}, "pro": {"daily_limit": 50, "monthly_limit": 1000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('ai_content_optimization', 'AI内容优化建议', 'per_request', 200, 'tokens_per_request',
 '{"starter": {"daily_limit": 5, "monthly_limit": 50}, "pro": {"daily_limit": 20, "monthly_limit": 500}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('ai_competitor_analysis', 'AI竞争对手分析', 'per_domain', 500, 'tokens_per_domain',
 '{"starter": {"daily_limit": 0, "monthly_limit": 0}, "pro": {"daily_limit": 3, "monthly_limit": 30}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('ai_keyword_research', 'AI关键词研究', 'per_keyword', 50, 'tokens_per_keyword',
 '{"starter": {"daily_limit": 10, "monthly_limit": 200}, "pro": {"daily_limit": 50, "monthly_limit": 1000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

-- SEO优化相关
('seo_basic_audit', '基础SEO审核', 'per_page', 50, 'tokens_per_page',
 '{"starter": {"daily_limit": 20, "monthly_limit": 300}, "pro": {"daily_limit": 100, "monthly_limit": 2000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('seo_technical_analysis', '技术SEO分析', 'per_domain', 300, 'tokens_per_domain',
 '{"starter": {"daily_limit": 5, "monthly_limit": 50}, "pro": {"daily_limit": 20, "monthly_limit": 200}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('seo_backlink_analysis', '外链分析', 'per_domain', 400, 'tokens_per_domain',
 '{"starter": {"daily_limit": 0, "monthly_limit": 0}, "pro": {"daily_limit": 10, "monthly_limit": 100}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

-- 批量操作相关
('batch_page_analysis', '批量页面分析', 'per_page', 80, 'tokens_per_page',
 '{"starter": {"daily_limit": 0, "monthly_limit": 0}, "pro": {"daily_limit": 100, "monthly_limit": 2000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('batch_keyword_research', '批量关键词研究', 'per_keyword', 30, 'tokens_per_keyword',
 '{"starter": {"daily_limit": 0, "monthly_limit": 0}, "pro": {"daily_limit": 200, "monthly_limit": 5000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

-- 监控相关
('competitor_monitoring', '竞争对手监控', 'per_domain_per_day', 100, 'tokens_per_domain_per_day',
 '{"starter": {"max_domains": 0}, "pro": {"max_domains": 5}, "elite": {"max_domains": -1}}', true),

('keyword_ranking_tracking', '关键词排名跟踪', 'per_keyword', 10, 'tokens_per_keyword_per_check',
 '{"starter": {"max_keywords": 50}, "pro": {"max_keywords": 500}, "elite": {"max_keywords": -1}}', true),

-- 报告相关
('custom_report_generation', '自定义报告生成', 'per_report', 300, 'tokens_per_report',
 '{"starter": {"daily_limit": 0, "monthly_limit": 0}, "pro": {"daily_limit": 5, "monthly_limit": 50}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('automated_report_scheduling', '自动化报告调度', 'per_report', 100, 'tokens_per_report',
 '{"starter": {"max_scheduled": 0}, "pro": {"max_scheduled": 5}, "elite": {"max_scheduled": -1}}', true),

-- API访问相关
('api_request', 'API请求', 'per_request', 1, 'tokens_per_request',
 '{"starter": {"daily_limit": 100, "monthly_limit": 2000}, "pro": {"daily_limit": 1000, "monthly_limit": 20000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true),

('api_data_export', 'API数据导出', 'per_1000_records', 50, 'tokens_per_1000_records',
 '{"starter": {"daily_limit": 10, "monthly_limit": 100}, "pro": {"daily_limit": 100, "monthly_limit": 1000}, "elite": {"daily_limit": -1, "monthly_limit": -1}}', true)

ON CONFLICT (feature_key) DO NOTHING;

-- 4. 创建系统配置表
CREATE TABLE IF NOT EXISTS billing.system_configurations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    config_key TEXT UNIQUE NOT NULL,
    config_value JSONB NOT NULL,
    description TEXT,
    config_category TEXT DEFAULT 'general',
    is_public BOOLEAN DEFAULT false, -- 是否对前端公开
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

-- 创建自动更新时间戳的函数
CREATE OR REPLACE FUNCTION billing.update_system_config_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 创建触发器
CREATE TRIGGER update_system_configurations_updated_at
    BEFORE UPDATE ON billing.system_configurations
    FOR EACH ROW EXECUTE FUNCTION billing.update_system_config_updated_at();

-- 5. 插入系统配置数据
INSERT INTO billing.system_configurations (config_key, config_value, description, config_category, is_public) VALUES
-- 基础配置
('system.version', '"1.0.0"', '系统版本号', 'system', true),
('system.maintenance', 'false', '系统维护状态', 'system', true),
('system.max_file_upload_size', '10485760', '最大文件上传大小(字节)', 'system', true),

-- Token配置
('token.daily_bonus_amount', '100', '每日登录奖励Token数量', 'token', false),
('token.referral_bonus_amount', '1000', '推荐奖励Token数量', 'token', false),
('token.minimum_balance_alert', '1000', 'Token余额预警阈值', 'token', false),

-- 项目限制
('project.max_domains_per_project_starter', '5', 'Starter套餐每项目最大域名数', 'project', false),
('project.max_domains_per_project_pro', '20', 'Pro套餐每项目最大域名数', 'project', false),
('project.max_domains_per_project_elite', '-1', 'Elite套餐每项目最大域名数(-1表示无限制)', 'project', false),

-- API配置
('api.rate_limit_per_minute', '60', 'API每分钟请求限制', 'api', false),
('api.max_concurrent_requests', '10', 'API最大并发请求数', 'api', false),
('api.timeout_seconds', '30', 'API请求超时时间(秒)', 'api', false),

-- 功能开关
('features.ai_analysis_enabled', 'true', 'AI分析功能开关', 'feature', true),
('features.competitor_monitoring_enabled', 'true', '竞争对手监控功能开关', 'feature', true),
('features.batch_analysis_enabled', 'true', '批量分析功能开关', 'feature', true),
('features.custom_reports_enabled', 'true', '自定义报告功能开关', 'feature', true),

-- 通知配置
('notifications.email_enabled', 'true', '邮件通知开关', 'notification', false),
('notifications.system_updates_enabled', 'true', '系统更新通知开关', 'notification', false),
('notifications.billing_reminders_enabled', 'true', '账单提醒通知开关', 'notification', false)

ON CONFLICT (config_key) DO NOTHING;

-- 6. 记录迁移完成日志
INSERT INTO public.schema_migrations (version, description, applied_at)
VALUES ('002', 'Initialize base data including pricing plans, token consumption rules, and system configurations', now())
ON CONFLICT (version) DO NOTHING;

COMMIT;

-- 显示初始化结果
DO $$
DECLARE
    plan_count INTEGER;
    rule_count INTEGER;
    config_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO plan_count FROM billing.pricing_plans WHERE is_active = true;
    SELECT COUNT(*) INTO rule_count FROM billing.token_consumption_rules WHERE is_active = true;
    SELECT COUNT(*) INTO config_count FROM billing.system_configurations;

    RAISE NOTICE '✅ 基础数据初始化迁移完成';
    RAISE NOTICE '📊 活跃价格计划数量: %', plan_count;
    RAISE NOTICE '📊 Token消耗规则数量: %', rule_count;
    RAISE NOTICE '📊 系统配置项数量: %', config_count;
    RAISE NOTICE '🎯 支持的套餐: Starter, Pro, Elite';
    RAISE NOTICE '💰 年费折扣: 50%';
    RAISE NOTICE '🎉 基础数据初始化成功完成！';
END $$;