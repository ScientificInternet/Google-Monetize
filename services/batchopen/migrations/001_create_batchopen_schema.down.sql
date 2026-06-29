-- Down migration: create_batchopen_schema
-- 回滚Batchopen域的所有表和对象
-- 注意：此操作会删除所有任务数据和配置，请谨慎执行

-- 删除视图
DROP VIEW IF EXISTS batchopen.template_usage_stats;
DROP VIEW IF EXISTS batchopen.queue_status;
DROP VIEW IF EXISTS batchopen.task_summary;

-- 删除触发器
DROP TRIGGER IF EXISTS update_task_queue_updated_at ON batchopen.task_queue;
DROP TRIGGER IF EXISTS update_resource_pools_updated_at ON batchopen.resource_pools;
DROP TRIGGER IF EXISTS update_task_templates_updated_at ON batchopen.task_templates;
DROP TRIGGER IF EXISTS update_tasks_updated_at ON batchopen.tasks;
DROP FUNCTION IF EXISTS update_updated_at_column();

-- 删除索引（先删除，再删除表）
DROP INDEX IF EXISTS idx_batchopen_pools_health;
DROP INDEX IF EXISTS idx_batchopen_pools_active;
DROP INDEX IF EXISTS idx_batchopen_pools_type;
DROP INDEX IF EXISTS idx_batchopen_queue_retry_at;
DROP INDEX IF EXISTS idx_batchopen_queue_scheduled_at;
DROP INDEX IF EXISTS idx_batchopen_queue_priority;
DROP INDEX IF EXISTS idx_batchopen_queue_status;
DROP INDEX IF EXISTS idx_batchopen_dependencies_child;
DROP INDEX IF EXISTS idx_batchopen_dependencies_parent;
DROP INDEX IF EXISTS idx_batchopen_templates_usage_count;
DROP INDEX IF EXISTS idx_batchopen_templates_active;
DROP INDEX IF EXISTS idx_batchopen_templates_category;
DROP INDEX IF EXISTS idx_batchopen_executions_executed_at;
DROP INDEX IF EXISTS idx_batchopen_executions_status;
DROP INDEX IF EXISTS idx_batchopen_executions_type;
DROP INDEX IF EXISTS idx_batchopen_executions_task_id;
DROP INDEX IF EXISTS idx_batchopen_tasks_type_status;
DROP INDEX IF EXISTS idx_batchopen_tasks_scheduled_at;
DROP INDEX IF EXISTS idx_batchopen_tasks_created_at;
DROP INDEX IF EXISTS idx_batchopen_tasks_priority;
DROP INDEX IF EXISTS idx_batchopen_tasks_status;
DROP INDEX IF EXISTS idx_batchopen_tasks_offer_id;
DROP INDEX IF EXISTS idx_batchopen_tasks_user_id;

-- 删除表（按依赖关系倒序）
DROP TABLE IF EXISTS batchopen.resource_pools;
DROP TABLE IF EXISTS batchopen.task_queue;
DROP TABLE IF EXISTS batchopen.task_dependencies;
DROP TABLE IF EXISTS batchopen.task_templates;
DROP TABLE IF EXISTS batchopen.task_executions;
DROP TABLE IF EXISTS batchopen.tasks;

-- 删除schema（如果为空）
DROP SCHEMA IF EXISTS batchopen;