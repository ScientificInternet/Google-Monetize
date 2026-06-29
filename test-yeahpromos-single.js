#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

async function testYeahpromos() {
  console.log('测试 yeahpromos.com 重定向追踪\n');

  const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      url: 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
      targetCountry: 'US',
      refererStrategy: 'social',
      proxyProviderURL: PROXY_URL_US,
      proxyPoolSize: 1,
      maxRetries: 1,
      stabilizeMs: 30000 // 增加到30秒
    })
  });

  const data = await response.json();
  console.log('结果:', JSON.stringify(data, null, 2));
}

testYeahpromos().catch(console.error);
