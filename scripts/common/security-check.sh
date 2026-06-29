#!/bin/bash

# 简单实用的安全检查脚本
# 遵循 MustKnow.md 原则：简单实用，不过度设计

echo "🔍 运行安全检查..."

# 错误计数
ERROR_COUNT=0

# 1. 检查硬编码的实际敏感值（排除变量名、配置键等）
echo "   检查硬编码敏感信息..."
# 使用更精确的匹配，只查找实际的值
HARDCODED_SECRETS=$(grep -r -E "(postgresql://(postgres|user):[^@]+@|redis://(default|user):[^@]+@|[a-f0-9]{64}|[A-Za-z0-9_-]{20,}apps\.googleusercontent\.com|GOCSPX-[A-Za-z0-9_-]+)" --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" --exclude-dir=node_modules --exclude-dir=.next --exclude-dir=__tests__ . | \
    grep -v "process.env" | \
    grep -v "NEXT_PUBLIC_" | \
    grep -v "your-" | \
    grep -v "example" | \
    grep -v "e\.g\." | \
    grep -v "postgresql://user:password@" | \
    grep -v "placeholder" | \
    grep -v "config.*example" | \
    grep -v "//.*secret" | \
    grep -v "your-secret" | \
    grep -v "secret-key" | \
    grep -v "32-character" | \
    grep -v "very-secure")

if [ -n "$HARDCODED_SECRETS" ]; then
    echo "❌ 发现硬编码的敏感信息！"
    echo "$HARDCODED_SECRETS"
    echo ""
    echo "请使用环境变量而不是硬编码敏感信息。"
    ERROR_COUNT=$((ERROR_COUNT + 1))
fi

# 2. 检查是否有已知的泄漏密码
echo "   检查已知泄漏的密码..."
LEAKED_PASSWORDS=$(grep -r -E "(${REDACTED_PASSWORD}|${REDACTED_PASSWORD}|${REDACTED_HASH}|${GOOGLE_OAUTH_CLIENT_ID}|${GOOGLE_OAUTH_CLIENT_SECRET})" --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" --exclude-dir=node_modules --exclude-dir=.next --exclude-dir=test-*.js .)

if [ -n "$LEAKED_PASSWORDS" ]; then
    echo "❌ 发现已知的泄漏密码！"
    echo "$LEAKED_PASSWORDS"
    ERROR_COUNT=$((ERROR_COUNT + 1))
fi

# 3. 检查 console.log（仅警告）
echo "   检查调试代码..."
CONSOLE_COUNT=$(grep -r "console.log" --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" --exclude-dir=node_modules --exclude-dir=.next --exclude-dir=__tests__ . | wc -l)
if [ "$CONSOLE_COUNT" -gt 10 ]; then
    echo "⚠️  发现过多的 console.log ($CONSOLE_COUNT 个)，建议清理"
fi

# 输出结果
if [ $ERROR_COUNT -gt 0 ]; then
    echo ""
    echo "🚫 安全检查失败！发现 $ERROR_COUNT 个问题"
    echo ""
    echo "请修复以上问题后重试"
    exit 1
else
    echo "✅ 安全检查通过"
fi