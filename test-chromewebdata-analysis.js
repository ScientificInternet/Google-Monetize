#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=3&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F';

async function analyzeChrome WebdataPage() {
  console.log('='.repeat(80));
  console.log('分析 chromewebdata 页面内容');
  console.log('='.repeat(80));

  // 使用更长的稳定化时间
  const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url: TEST_URL,
      targetCountry: 'US',
      refererStrategy: 'social',
      proxyProviderURL: PROXY_URL,
      proxyPoolSize: 3,
      maxRetries: 1,
      advancedOptions: {
        stabilizeMs: 15000  // 增加到15秒
      }
    })
  });

  const visitData = await visitResp.json();

  console.log('\n基本信息:');
  console.log('- 最终域名:', visitData.result?.domain);
  console.log('- 状态码:', visitData.result?.statusCode);
  console.log('- 是否中间页:', visitData.result?.isIntermediatePage);
  console.log('- 稳定化耗时:', visitData.timings?.stabilizationMs, 'ms');

  console.log('\n重定向链路:');
  if (visitData.result?.redirectChain) {
    visitData.result.redirectChain.forEach((step, idx) => {
      console.log(`${idx + 1}. ${new URL(step.url).hostname}`);
    });
  }

  console.log('\n' + '='.repeat(80));
}

analyzeChrome WebdataPage().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
