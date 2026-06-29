#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=10&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F';

async function testMultipleTimes(times = 5) {
  console.log('='.repeat(80));
  console.log(`Stealth 插件优化效果测试 - 连续测试 ${times} 次`);
  console.log('='.repeat(80));

  const results = {
    total: times,
    success: 0,
    failed: 0,
    reachedFinalPage: 0,
    stuckAtChromewebdata: 0,
    timeout: 0,
    durations: []
  };

  for (let i = 1; i <= times; i++) {
    console.log(`\n[测试 ${i}/${times}] 开始...`);

    try {
      const t0 = Date.now();
      const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: TEST_URL,
          targetCountry: 'US',
          refererStrategy: 'social',
          proxyProviderURL: PROXY_URL,
          proxyPoolSize: 10,
          maxRetries: 1
        })
      });

      const visitData = await visitResp.json();
      const duration = Date.now() - t0;
      results.durations.push(duration);

      if (visitData.success) {
        results.success++;

        if (visitData.result?.domain === 'dyson.hr') {
          results.reachedFinalPage++;
          console.log(`✅ 成功到达最终落地页: ${visitData.result.finalUrl}`);
          console.log(`   - 域名: ${visitData.result.domain}`);
          console.log(`   - 品牌: ${visitData.result.brandName}`);
          console.log(`   - 可用: ${visitData.result.available}`);
          console.log(`   - 耗时: ${visitData.timings.totalMs}ms`);
        } else if (visitData.result?.domain === 'chromewebdata') {
          results.stuckAtChromewebdata++;
          console.log(`⚠️  卡在中间页: chromewebdata`);
          console.log(`   - 耗时: ${visitData.timings.totalMs}ms`);
        } else {
          console.log(`ℹ️  访问成功但域名异常: ${visitData.result?.domain}`);
        }
      } else {
        results.failed++;

        if (visitData.error?.type === 'timeout') {
          results.timeout++;
          console.log(`❌ 超时失败`);
        } else {
          console.log(`❌ 失败: ${visitData.error?.message?.slice(0, 100)}`);
        }
      }

      await new Promise(r => setTimeout(r, 1000));

    } catch (err) {
      results.failed++;
      console.log(`❌ 请求失败: ${err.message}`);
    }
  }

  console.log('\n' + '='.repeat(80));
  console.log('测试结果统计');
  console.log('='.repeat(80));

  console.log(`\n总测试次数: ${results.total}`);
  console.log(`成功次数: ${results.success} (${(results.success / results.total * 100).toFixed(1)}%)`);
  console.log(`失败次数: ${results.failed} (${(results.failed / results.total * 100).toFixed(1)}%)`);
  console.log(`  - 超时: ${results.timeout}`);

  console.log(`\n重定向完整性:`);
  console.log(`✅ 到达最终落地页 (dyson.hr): ${results.reachedFinalPage} 次`);
  console.log(`⚠️  卡在中间页 (chromewebdata): ${results.stuckAtChromewebdata} 次`);

  if (results.durations.length > 0) {
    const avgDuration = results.durations.reduce((a, b) => a + b, 0) / results.durations.length;
    const minDuration = Math.min(...results.durations);
    const maxDuration = Math.max(...results.durations);

    console.log(`\n耗时统计:`);
    console.log(`平均: ${avgDuration.toFixed(0)}ms`);
    console.log(`最快: ${minDuration}ms`);
    console.log(`最慢: ${maxDuration}ms`);
  }

  console.log('\n' + '='.repeat(80));

  if (results.reachedFinalPage >= results.total * 0.8) {
    console.log('✅ 测试通过！80%以上成功到达最终落地页');
  } else if (results.reachedFinalPage > 0) {
    console.log('⚠️  部分成功，stealth优化有效但需要进一步调优');
  } else {
    console.log('❌ 测试失败，未能到达最终落地页');
  }

  console.log('='.repeat(80));
}

testMultipleTimes(5).catch(err => {
  console.error('测试执行失败:', err);
  process.exit(1);
});
