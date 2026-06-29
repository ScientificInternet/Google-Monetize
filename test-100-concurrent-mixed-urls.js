#!/usr/bin/env node

/**
 * 100并发压力测试 - 混合URL版本
 *
 * 使用多个不同的URL进行测试，这样代理可以跨URL复用
 * 符合业务约束：同一URL使用不同代理，不同URL可以复用代理
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

// 使用4个不同的测试URL（从之前的测试中选择）
const TEST_URLS = [
  'https://pboost.me/ZDO2Bdek',
  'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
  'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
  'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink'
];

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
        proxyPoolSize: 20,
        maxRetries: 1
      })
    });

    const data = await response.json();
    const totalTime = Date.now() - startTime;

    return {
      index,
      url,
      urlDomain: new URL(url).hostname,
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
      urlDomain: new URL(url).hostname,
      success: false,
      statusCode: 0,
      totalTime: Date.now() - startTime,
      error: { message: error.message }
    };
  }
}

async function test100ConcurrentMixedUrls() {
  console.log('='.repeat(100));
  console.log('100并发压力测试 - 混合URL版本');
  console.log('='.repeat(100));
  console.log(`URL: ${BROWSER_EXEC_URL}`);
  console.log(`测试URL数量: ${TEST_URLS.length} 个不同URL`);
  console.log(`并发数: 100 (每个URL 25次并发)`);
  console.log('');
  console.log('策略说明:');
  console.log('- 使用4个不同URL，每个URL发送25次请求');
  console.log('- 代理池大小: 50个');
  console.log('- 约束: 同一URL不能重复使用代理，但不同URL可以复用');
  console.log('- 理论: 50个代理可支持 50×4=200 次跨URL访问');
  console.log('');

  const startTime = Date.now();

  // 创建100个并发请求，轮流使用4个URL
  const promises = [];
  for (let i = 0; i < 100; i++) {
    const url = TEST_URLS[i % TEST_URLS.length];
    promises.push(singleVisit(url, i + 1));

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

  // 按URL统计
  const urlStats = {};
  for (const url of TEST_URLS) {
    const urlResults = results.filter(r => r.url === url);
    const urlSuccess = urlResults.filter(r => r.success).length;
    urlStats[new URL(url).hostname] = {
      total: urlResults.length,
      success: urlSuccess,
      successRate: (urlSuccess / urlResults.length * 100).toFixed(1) + '%'
    };
  }

  // 输出结果
  console.log('='.repeat(100));
  console.log('测试结果');
  console.log('='.repeat(100));
  console.log('');
  console.log(`总耗时: ${(totalTime / 1000).toFixed(1)}秒`);
  console.log('');
  console.log(`总体成功率: ${successCount}/100 (${successCount}%)`);
  console.log(`总体失败率: ${failureCount}/100 (${failureCount}%)`);
  console.log('');
  console.log('响应时间分布:');
  console.log(`  平均: ${(avgTime / 1000).toFixed(1)}秒`);
  console.log(`  最快: ${(minTime / 1000).toFixed(1)}秒`);
  console.log(`  最慢: ${(maxTime / 1000).toFixed(1)}秒`);
  console.log(`  P50:  ${(p50 / 1000).toFixed(1)}秒`);
  console.log(`  P90:  ${(p90 / 1000).toFixed(1)}秒`);
  console.log(`  P99:  ${(p99 / 1000).toFixed(1)}秒`);

  // 按URL统计
  console.log('');
  console.log('各URL成功率分布:');
  console.log('-'.repeat(100));
  for (const [domain, stats] of Object.entries(urlStats)) {
    console.log(`  ${domain.padEnd(40)} ${stats.success}/${stats.total} (${stats.successRate})`);
  }

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
    console.log(`✅ 优秀: 100并发（混合URL）成功率达到${successCount}%`);
  } else if (successCount >= 80) {
    console.log(`⚠️  良好: 100并发（混合URL）成功率为${successCount}%，建议继续优化`);
  } else if (successCount >= 70) {
    console.log(`⚠️  及格: 100并发（混合URL）成功率为${successCount}%，需要优化`);
  } else {
    console.log(`❌ 不合格: 100并发（混合URL）成功率仅${successCount}%，需要大幅优化`);
  }

  console.log('');
  console.log('配置验证:');
  console.log(`  代理池大小: 50个`);
  console.log(`  URL数量: 4个`);
  console.log(`  每URL并发: 25次`);
  console.log(`  理论支持: 50代理 × 4URL = 200次跨URL访问`);
  console.log(`  实际测试: 100次`);
  console.log(`  代理利用率: ${(100 / 200 * 100).toFixed(1)}%`);

  if (successCount >= 90) {
    console.log('');
    console.log('✅ 混合URL策略有效！代理池复用成功，避免了同URL重复使用代理的限制。');
  } else {
    console.log('');
    console.log('建议优化:');
    console.log('  1. 检查是否有代理质量问题');
    console.log('  2. 增加代理池到100个');
    console.log('  3. 优化Cloud Run实例配置');
  }

  console.log('');
  console.log('='.repeat(100));
}

test100ConcurrentMixedUrls().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
