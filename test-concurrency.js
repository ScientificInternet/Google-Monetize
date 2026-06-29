#!/usr/bin/env node

/**
 * Browser-Exec Concurrency Stress Test
 *
 * 测试并发访问能力，逐步增加并发数
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

// 测试用的URL（选择较快的）
const TEST_URLS = [
  'https://pboost.me/ZDO2Bdek',
  'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
];

/**
 * 单次访问测试
 */
async function singleVisit(url, index) {
  const startTime = Date.now();

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 10,
        maxRetries: 1
      })
    });

    const data = await response.json();
    const totalTime = Date.now() - startTime;

    return {
      index,
      url,
      success: data.success,
      statusCode: response.status,
      totalTime,
      finalUrl: data.result?.finalUrl,
      error: data.error
    };
  } catch (error) {
    return {
      index,
      url,
      success: false,
      statusCode: 0,
      totalTime: Date.now() - startTime,
      error: { message: error.message }
    };
  }
}

/**
 * 并发测试
 */
async function concurrentTest(concurrency) {
  console.log(`\n${'='.repeat(100)}`);
  console.log(`并发测试: ${concurrency} 个并发请求`);
  console.log('='.repeat(100));

  const startTime = Date.now();

  // 创建并发请求（轮流使用测试URL）
  const promises = [];
  for (let i = 0; i < concurrency; i++) {
    const url = TEST_URLS[i % TEST_URLS.length];
    promises.push(singleVisit(url, i + 1));
  }

  // 等待所有请求完成
  const results = await Promise.all(promises);
  const totalTime = Date.now() - startTime;

  // 统计结果
  const successCount = results.filter(r => r.success).length;
  const failureCount = results.filter(r => !r.success).length;
  const avgTime = Math.round(results.reduce((sum, r) => sum + r.totalTime, 0) / results.length);
  const minTime = Math.min(...results.map(r => r.totalTime));
  const maxTime = Math.max(...results.map(r => r.totalTime));

  // 输出结果
  console.log(`\n测试完成 (总耗时: ${(totalTime / 1000).toFixed(1)}秒)`);
  console.log('-'.repeat(100));
  console.log(`成功: ${successCount}/${concurrency} (${(successCount / concurrency * 100).toFixed(1)}%)`);
  console.log(`失败: ${failureCount}/${concurrency} (${(failureCount / concurrency * 100).toFixed(1)}%)`);
  console.log('');
  console.log(`响应时间:`);
  console.log(`  平均: ${(avgTime / 1000).toFixed(1)}秒`);
  console.log(`  最快: ${(minTime / 1000).toFixed(1)}秒`);
  console.log(`  最慢: ${(maxTime / 1000).toFixed(1)}秒`);

  // 显示失败详情
  if (failureCount > 0) {
    console.log('\n失败详情:');
    results.filter(r => !r.success).forEach(r => {
      console.log(`  #${r.index}: ${r.error?.message || 'Unknown error'} (${r.statusCode})`);
    });
  }

  // 显示成功详情
  console.log('\n成功详情:');
  results.filter(r => r.success).slice(0, 5).forEach(r => {
    console.log(`  #${r.index}: ${r.finalUrl?.slice(0, 50)}... (${(r.totalTime / 1000).toFixed(1)}s)`);
  });
  if (successCount > 5) {
    console.log(`  ... 还有 ${successCount - 5} 个成功请求`);
  }

  return {
    concurrency,
    totalTime,
    successCount,
    failureCount,
    successRate: successCount / concurrency,
    avgTime,
    minTime,
    maxTime
  };
}

/**
 * 主测试函数
 */
async function main() {
  console.log('='.repeat(100));
  console.log('Browser-Exec 并发压力测试');
  console.log('='.repeat(100));
  console.log(`测试环境: ${BROWSER_EXEC_URL}`);
  console.log(`测试时间: ${new Date().toISOString()}`);
  console.log(`测试URL: ${TEST_URLS.length} 个`);
  console.log('');

  const testCases = [
    { concurrency: 2, description: '低并发 (2个)' },
    { concurrency: 4, description: '中等并发 (4个)' },
    { concurrency: 8, description: '高并发 (8个)' },
    { concurrency: 12, description: '极高并发 (12个)' }
  ];

  const allResults = [];

  for (const testCase of testCases) {
    console.log(`\n📊 测试 ${testCase.description}...`);
    const result = await concurrentTest(testCase.concurrency);
    allResults.push(result);

    // 等待5秒让服务恢复
    if (testCase !== testCases[testCases.length - 1]) {
      console.log('\n⏳ 等待5秒让服务恢复...');
      await new Promise(r => setTimeout(r, 5000));
    }
  }

  // 总结报告
  console.log('\n' + '='.repeat(100));
  console.log('测试总结');
  console.log('='.repeat(100));
  console.log('');
  console.log('并发性能对比:');
  console.log('-'.repeat(100));
  console.log('并发数 | 成功率 | 平均响应时间 | 最快 | 最慢 | 总耗时');
  console.log('-'.repeat(100));

  allResults.forEach(r => {
    console.log(
      `${String(r.concurrency).padStart(6)} | ` +
      `${(r.successRate * 100).toFixed(1).padStart(5)}% | ` +
      `${(r.avgTime / 1000).toFixed(1).padStart(13)}s | ` +
      `${(r.minTime / 1000).toFixed(1).padStart(4)}s | ` +
      `${(r.maxTime / 1000).toFixed(1).padStart(4)}s | ` +
      `${(r.totalTime / 1000).toFixed(1).padStart(6)}s`
    );
  });

  console.log('');

  // 性能评估
  const maxConcurrency = allResults[allResults.length - 1].concurrency;
  const maxSuccessRate = allResults[allResults.length - 1].successRate;

  console.log('性能评估:');
  console.log('-'.repeat(100));

  if (maxSuccessRate >= 0.95) {
    console.log(`✅ 优秀: 在${maxConcurrency}并发下成功率达到${(maxSuccessRate * 100).toFixed(1)}%`);
  } else if (maxSuccessRate >= 0.8) {
    console.log(`⚠️  良好: 在${maxConcurrency}并发下成功率为${(maxSuccessRate * 100).toFixed(1)}%`);
  } else {
    console.log(`❌ 需优化: 在${maxConcurrency}并发下成功率仅${(maxSuccessRate * 100).toFixed(1)}%`);
  }

  // 找出最佳并发数
  const bestResult = allResults
    .filter(r => r.successRate >= 0.9)
    .sort((a, b) => b.concurrency - a.concurrency)[0];

  if (bestResult) {
    console.log(`\n推荐并发数: ${bestResult.concurrency} (成功率${(bestResult.successRate * 100).toFixed(1)}%)`);
  }

  // Cloud Run配置建议
  console.log('\n配置建议:');
  console.log('-'.repeat(100));
  console.log(`当前配置: concurrency=4, min-instances=1, max-instances=10`);

  if (maxSuccessRate >= 0.95 && maxConcurrency >= 8) {
    console.log(`✅ 当前配置良好，可以处理${maxConcurrency}+并发`);
  } else if (maxSuccessRate < 0.8) {
    console.log(`⚠️  建议调整:`);
    console.log(`   - 增加 min-instances 到 2-3 (减少冷启动)`);
    console.log(`   - 保持 concurrency=4 (避免单实例过载)`);
    console.log(`   - 监控代理池大小 (确保足够代理)`);
  }

  console.log('');
  console.log('='.repeat(100));
  console.log('测试完成');
  console.log('='.repeat(100));
}

main().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
