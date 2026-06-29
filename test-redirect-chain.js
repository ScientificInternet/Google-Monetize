#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=5&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F';

async function testRedirectChain() {
  console.log('='.repeat(80));
  console.log('重定向链路详细测试');
  console.log('='.repeat(80));

  // Clear previous proxy usage
  await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/proxy-pool/clear-usage`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: TEST_URL })
  });

  const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url: TEST_URL,
      targetCountry: 'US',
      refererStrategy: 'social',
      proxyProviderURL: PROXY_URL,
      proxyPoolSize: 5,
      maxRetries: 1
    })
  });

  const visitData = await visitResp.json();

  console.log('\n访问结果详情:');
  console.log('- 成功:', visitData.success);
  console.log('- 状态码:', visitData.result?.statusCode);
  console.log('- 最终URL:', visitData.result?.finalUrl);
  console.log('- 域名:', visitData.result?.domain);
  console.log('- 品牌:', visitData.result?.brandName);
  console.log('- 是否可用:', visitData.result?.available);
  console.log('- 是否中间页:', visitData.result?.isIntermediatePage);
  console.log('- 失败原因:', visitData.result?.failureReason);

  console.log('\n重定向链路:');
  if (visitData.result?.redirectChain) {
    visitData.result.redirectChain.forEach((step, idx) => {
      const url = new URL(step.url || step);
      console.log(`${idx + 1}. ${url.hostname} - ${step.url || step}`);
    });
  }

  console.log('\n元数据:');
  console.log('- 代理:', visitData.metadata?.proxyServer?.split(':').slice(0, 2).join(':'));
  console.log('- Referer:', visitData.metadata?.referer);
  console.log('- UserAgent:', visitData.metadata?.userAgent?.slice(0, 50) + '...');

  console.log('\n耗时统计:');
  console.log('- 导航:', visitData.timings?.navigationMs, 'ms');
  console.log('- 稳定化:', visitData.timings?.stabilizationMs, 'ms');
  console.log('- 重定向次数:', visitData.timings?.redirectCount);
  console.log('- 总计:', visitData.timings?.totalMs, 'ms');

  console.log('\n反爬虫检测:');
  console.log(JSON.stringify(visitData.result?.antiDetectionResult, null, 2));

  console.log('\n完整响应:');
  console.log(JSON.stringify(visitData, null, 2));

  console.log('\n' + '='.repeat(80));
}

testRedirectChain().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
