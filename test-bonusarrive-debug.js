#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink';

async function testBonusarrive() {
  console.log('='.repeat(80));
  console.log('测试 bonusarrive.com 访问 (多次尝试)');
  console.log('='.repeat(80));
  console.log(`测试URL: ${TEST_URL}`);
  console.log('');

  for (let attempt = 1; attempt <= 3; attempt++) {
    console.log(`\n${'─'.repeat(80)}`);
    console.log(`[尝试 ${attempt}/3] 开始测试...`);
    console.log('─'.repeat(80));

    try {
      const t0 = Date.now();
      const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: TEST_URL,
          targetCountry: 'US',
          refererStrategy: 'social',
          proxyProviderURL: PROXY_URL_US,
          proxyPoolSize: 1,
          maxRetries: 1
        })
      });

      const visitData = await visitResp.json();
      const totalTime = Date.now() - t0;

      console.log(`\n访问结果:`);
      console.log(`  成功: ${visitData.success}`);
      console.log(`  总耗时: ${totalTime}ms`);

      if (visitData.success) {
        const result = visitData.result || {};

        console.log(`\n最终页面:`);
        console.log(`  最终URL: ${result.finalUrl}`);
        console.log(`  域名: ${result.domain}`);
        console.log(`  品牌: ${result.brandName}`);
        console.log(`  状态码: ${result.statusCode}`);
        console.log(`  是否中间页: ${result.isIntermediatePage}`);
        console.log(`  可用性: ${result.available}`);

        if (result.redirectChain && result.redirectChain.length > 0) {
          console.log(`\n重定向链路 (${result.redirectChain.length}跳):`);
          result.redirectChain.forEach((step, idx) => {
            const url = typeof step === 'string' ? step : step.url;
            const urlObj = new URL(url);
            console.log(`  ${idx + 1}. ${urlObj.hostname}${urlObj.pathname.slice(0, 50)}`);
          });
        }

        if (visitData.timings) {
          console.log(`\n性能:`);
          console.log(`  导航: ${visitData.timings.navigationMs}ms`);
          console.log(`  稳定化: ${visitData.timings.stabilizationMs}ms`);
          console.log(`  重定向: ${visitData.timings.redirectCount || 0}次`);
        }

        if (result.antiDetectionResult) {
          console.log(`\n反检测结果:`);
          console.log(`  通过: ${result.antiDetectionResult.passed}`);
          if (!result.antiDetectionResult.passed) {
            console.log(`  拦截方: ${result.antiDetectionResult.blockedBy}`);
          }
        }

      } else {
        console.log(`\n❌ 访问失败:`);
        console.log(`  错误类型: ${visitData.error?.type}`);
        console.log(`  错误信息: ${visitData.error?.message?.slice(0, 200)}`);

        if (visitData.error?.type === 'antibot') {
          console.log(`\n  ⚠️  被反机器人系统拦截`);
          console.log(`  拦截方: ${visitData.error?.message}`);
        }

        if (visitData.result?.antiDetectionResult) {
          console.log(`\n  反检测详情:`);
          console.log(`    通过: ${visitData.result.antiDetectionResult.passed}`);
          console.log(`    拦截方: ${visitData.result.antiDetectionResult.blockedBy || 'N/A'}`);
        }
      }

      if (visitData.metadata) {
        console.log(`\n访问配置:`);
        console.log(`  代理IP: ${visitData.metadata.proxyServer?.split(':').slice(0, 2).join(':') || 'N/A'}`);
        console.log(`  Referer: ${visitData.metadata.referer || 'N/A'}`);
      }

    } catch (err) {
      console.log(`\n❌ 请求异常: ${err.message}`);
    }

    if (attempt < 3) {
      console.log(`\n等待3秒后重试...`);
      await new Promise(r => setTimeout(r, 3000));
    }
  }

  console.log('\n' + '='.repeat(80));
  console.log('测试完成');
  console.log('='.repeat(80));
}

testBonusarrive().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
