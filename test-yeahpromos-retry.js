#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=';

async function testYeahpromos() {
  console.log('='.repeat(80));
  console.log('重新测试 yeahpromos.com (多次尝试)');
  console.log('='.repeat(80));

  for (let i = 1; i <= 3; i++) {
    console.log(`\n[尝试 ${i}/3] 测试开始...`);

    try {
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

      console.log(`\n结果:`);
      console.log(`  成功: ${visitData.success}`);

      if (visitData.success) {
        console.log(`  最终URL: ${visitData.result?.finalUrl}`);
        console.log(`  域名: ${visitData.result?.domain}`);
        console.log(`  状态码: ${visitData.result?.statusCode}`);
        console.log(`  是否中间页: ${visitData.result?.isIntermediatePage}`);
        console.log(`  可用性: ${visitData.result?.available}`);
        console.log(`  失败原因: ${visitData.result?.failureReason || 'N/A'}`);

        if (visitData.result?.redirectChain) {
          console.log(`\n  重定向链路:`);
          visitData.result.redirectChain.forEach((step, idx) => {
            console.log(`    ${idx + 1}. ${step.url}`);
          });
        }

        // 判断是否是失效页
        const finalUrl = visitData.result?.finalUrl || '';
        if (finalUrl.includes('error_suspended') ||
            finalUrl.includes('error') && visitData.result?.domain?.includes('dailybacks')) {
          console.log(`\n  ✅ 正确识别为失效页！`);
        }
      } else {
        console.log(`  错误类型: ${visitData.error?.type}`);
        console.log(`  错误信息: ${visitData.error?.message}`);

        // 分析错误原因
        if (visitData.error?.message?.includes('ERR_SSL_PROTOCOL_ERROR')) {
          console.log(`\n  ⚠️  SSL协议错误 - 可能原因:`);
          console.log(`     1. 代理IP与目标网站SSL不兼容`);
          console.log(`     2. 网站SSL证书配置问题`);
          console.log(`     3. 代理IP被该网站封禁`);
        }
      }

      console.log(`\n  代理IP: ${visitData.metadata?.proxyServer?.split(':').slice(0, 2).join(':')}`);
      console.log(`  Referer: ${visitData.metadata?.referer}`);

    } catch (err) {
      console.log(`\n  ❌ 请求异常: ${err.message}`);
    }

    if (i < 3) {
      console.log(`\n  等待3秒后重试...`);
      await new Promise(r => setTimeout(r, 3000));
    }
  }

  console.log('\n' + '='.repeat(80));
}

testYeahpromos().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
