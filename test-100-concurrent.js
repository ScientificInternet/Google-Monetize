#!/usr/bin/env node

/**
 * 100并发压力测试
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

// 使用最快的URL
const TEST_URL = 'https://pboost.me/ZDO2Bdek';

async function singleVisit(index) {
  const startTime = Date.now();

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: TEST_URL,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 20,
        maxRetries: 1
      })
    });

    const data = await response.json();
    const totalTime = Date.now() - startTime;

    return {
      index,
      success: data.success,
      statusCode: response.status,
      totalTime,
      error: data.error
    };
  } catch (error) {
    return {
      index,
      success: false,
      statusCode: 0,
      totalTime: Date.now() - startTime,
      error: { message: error.message }
    };
  }
}

async function test100Concurrent() {
  console.log('='.repeat(100));
  console.log('100并发压力测试');
  console.log('='.repeat(100));
  console.log(`URL: ${BROWSER_EXEC_URL}`);
  console.log(`测试URL: ${TEST_URL}`);
  console.log(`并发数: 100`);
  console.log('');

  const startTime = Date.now();

  // 创建100个并发请求
  const promises = [];
  for (let i = 0; i < 100; i++) {
    promises.push(singleVisit(i + 1));

    // 每10个显示进度
    if ((i + 1) % 10 === 0) {
      console.log(`启动 ${i + 1}/100 个请求...`);
    }
  }

  console.log('\n⏳ 等待所有请求完成...\n');

  // 等待所有请求完成
  const results = await Promise.all(promises);
  const totalTime = Date.now() - startTime;

  // 统计结果
  const successCount = results.filter(r => r.success).length;
  const failureCount = results.filter(r => !r.success).length;
  const avgTime = Math.round(results.reduce((sum, r) => sum + r.totalTime, 0) / results.length);
  const minTime = Math.min(...results.map(r => r.totalTime));
  const maxTime = Math.max(...results.map(r => r.totalTime));

  // 按时间排序
  const sortedByTime = [...results].sort((a, b) => a.totalTime - b.totalTime);
  const p50 = sortedByTime[49].totalTime;
  const p90 = sortedByTime[89].totalTime;
  const p99 = sortedByTime[98].totalTime;

  // 输出结果
  console.log('='.repeat(100));
  console.log('测试结果');
  console.log('='.repeat(100));
  console.log('');
  console.log(`总耗时: ${(totalTime / 1000).toFixed(1)}秒`);
  console.log('');
  console.log(`成功率: ${successCount}/100 (${successCount}%)`);
  console.log(`失败率: ${failureCount}/100 (${failureCount}%)`);
  console.log('');
  console.log('响应时间分布:');
  console.log(`  平均: ${(avgTime / 1000).toFixed(1)}秒`);
  console.log(`  最快: ${(minTime / 1000).toFixed(1)}秒`);
  console.log(`  最慢: ${(maxTime / 1000).toFixed(1)}秒`);
  console.log(`  P50:  ${(p50 / 1000).toFixed(1)}秒`);
  console.log(`  P90:  ${(p90 / 1000).toFixed(1)}秒`);
  console.log(`  P99:  ${(p99 / 1000).toFixed(1)}秒`);

  // 错误分析
  if (failureCount > 0) {
    console.log('');
    console.log('失败原因分布:');
    const errorTypes = {};
    results.filter(r => !r.success).forEach(r => {
      const errorMsg = r.error?.message || 'Unknown';
      const shortMsg = errorMsg.slice(0, 80);
      errorTypes[shortMsg] = (errorTypes[shortMsg] || 0) + 1;
    });

    Object.entries(errorTypes)
      .sort((a, b) => b[1] - a[1])
      .forEach(([msg, count]) => {
        console.log(`  ${count}个: ${msg}`);
      });
  }

  // 性能评估
  console.log('');
  console.log('='.repeat(100));
  console.log('性能评估');
  console.log('='.repeat(100));
  console.log('');

  if (successCount >= 90) {
    console.log(`✅ 优秀: 100并发成功率达到${successCount}%`);
  } else if (successCount >= 80) {
    console.log(`⚠️  良好: 100并发成功率为${successCount}%，建议继续优化`);
  } else if (successCount >= 70) {
    console.log(`⚠️  及格: 100并发成功率为${successCount}%，需要优化`);
  } else {
    console.log(`❌ 不合格: 100并发成功率仅${successCount}%，需要大幅优化`);
  }

  console.log('');
  console.log('配置信息:');
  console.log(`  当前: 4Gi内存, 4CPU, concurrency=8, max-instances=15`);
  console.log(`  理论最大并发: 8 × 15 = 120`);
  console.log(`  实际测试并发: 100`);
  console.log(`  利用率: ${(100 / 120 * 100).toFixed(1)}%`);

  if (successCount >= 90) {
    console.log('');
    console.log('✅ 当前配置可以支持100并发！');
  } else {
    console.log('');
    console.log('建议优化:');
    console.log('  1. 增加max-instances到20 (理论160并发)');
    console.log('  2. 增加代理池到100');
    console.log('  3. 优化代理质量和健康检查');
  }

  console.log('');
  console.log('='.repeat(100));
}

test100Concurrent().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
