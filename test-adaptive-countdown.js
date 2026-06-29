#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

async function testOffer(name, url) {
  console.log(`\n测试 ${name}...`);
  const startTime = Date.now();

  const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url,
      targetCountry: 'US',
      refererStrategy: 'social',
      proxyProviderURL: PROXY_URL_US,
      proxyPoolSize: 1,
      maxRetries: 1
    })
  });

  const data = await response.json();
  const totalTime = Date.now() - startTime;

  console.log(`✓ 总耗时: ${totalTime}ms`);
  console.log(`✓ 导航耗时: ${data.timings?.navigationMs || 0}ms`);
  console.log(`✓ 稳定化耗时: ${data.timings?.stabilizationMs || 0}ms`);
  console.log(`✓ 重定向次数: ${data.timings?.redirectCount || 0}`);
  console.log(`✓ 最终URL: ${data.result?.finalUrl || 'N/A'}`);
  console.log(`✓ 品牌: ${data.result?.brandName || 'N/A'}`);
  
  return data;
}

// Test bonusarrive (has Cloudflare + multiple redirects)
testOffer(
  'bonusarrive.com',
  'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink'
).catch(console.error);
