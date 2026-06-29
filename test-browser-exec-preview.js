#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=10&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F';

async function testBrowserExec() {
  console.log('='.repeat(80));
  console.log('Browser-Exec 预发环境测试');
  console.log('='.repeat(80));

  // Test 1: 测试代理池获取
  console.log('\n[测试 1] 获取代理池状态...');
  try {
    const proxyResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/proxies/status?country=US`);
    const proxyData = await proxyResp.json();
    console.log('✅ 代理池状态:', JSON.stringify(proxyData, null, 2));
  } catch (err) {
    console.error('❌ 代理池状态获取失败:', err.message);
  }

  // Test 2: 导入代理IP池
  console.log('\n[测试 2] 导入代理IP池...');
  try {
    const proxyListResp = await fetch(PROXY_URL);
    const proxyListText = await proxyListResp.text();
    const proxyLines = proxyListText.split('\n').filter(l => l.trim());

    console.log(`获取到 ${proxyLines.length} 个代理IP`);
    console.log('代理IP示例:', proxyLines.slice(0, 2).map(p => p.split(':').slice(0, 2).join(':')));

    const importResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/proxies/import`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        country: 'US',
        lines: proxyLines
      })
    });
    const importData = await importResp.json();
    console.log('✅ 导入结果:', importData);
  } catch (err) {
    console.error('❌ 代理IP导入失败:', err.message);
  }

  // Test 3: 访问测试 - 第一次访问
  console.log('\n[测试 3] 第一次访问测试 (检查代理使用)...');
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

    console.log(`✅ 访问成功 (${duration}ms)`);
    console.log('状态码:', visitData.result?.statusCode);
    console.log('最终URL:', visitData.result?.finalUrl);
    console.log('域名:', visitData.result?.domain);
    console.log('品牌名:', visitData.result?.brandName);
    console.log('是否可用:', visitData.result?.available);
    console.log('使用代理:', visitData.metadata?.proxyServer?.split(':').slice(0, 2).join(':'));
    console.log('Referer:', visitData.metadata?.referer);
    console.log('重定向链:', visitData.result?.redirectChain?.length || 0, '跳');
    console.log('耗时统计:', {
      导航: visitData.timings?.navigationMs + 'ms',
      稳定化: visitData.timings?.stabilizationMs + 'ms',
      总计: visitData.timings?.totalMs + 'ms'
    });
  } catch (err) {
    console.error('❌ 访问失败:', err.message);
  }

  // Test 4: 第二次访问 - 验证代理不重复
  console.log('\n[测试 4] 第二次访问测试 (验证代理不重复)...');
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

    console.log(`✅ 访问成功 (${duration}ms)`);
    console.log('使用代理:', visitData.metadata?.proxyServer?.split(':').slice(0, 2).join(':'));
    console.log('最终URL:', visitData.result?.finalUrl);
    console.log('是否可用:', visitData.result?.available);
  } catch (err) {
    console.error('❌ 访问失败:', err.message);
  }

  // Test 5: 检查代理池使用统计
  console.log('\n[测试 5] 代理池使用统计...');
  try {
    const statsResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/proxy-pool/stats`);
    const statsData = await statsResp.json();
    console.log('✅ 代理池统计:', JSON.stringify(statsData, null, 2));
  } catch (err) {
    console.error('❌ 统计获取失败:', err.message);
  }

  // Test 6: 检查代理健康状态
  console.log('\n[测试 6] 代理健康状态...');
  try {
    const healthResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/proxy-pool/health`);
    const healthData = await healthResp.json();
    console.log('✅ 代理健康统计:');
    console.log('  总代理数:', healthData.totalProxies);
    console.log('  健康代理:', healthData.healthyProxies);
    console.log('  隔离代理:', healthData.quarantinedProxies);
    if (healthData.proxies && healthData.proxies.length > 0) {
      console.log('  前3个代理状态:');
      healthData.proxies.slice(0, 3).forEach(p => {
        console.log(`    - ${p.proxy}: 评分=${p.score}, 成功率=${(p.successRate * 100).toFixed(1)}%, 平均响应=${p.avgResponseTime}ms, 层级=${p.tier}`);
      });
    }
  } catch (err) {
    console.error('❌ 健康状态获取失败:', err.message);
  }

  // Test 7: 测试风控拦截快速失败
  console.log('\n[测试 7] 测试风控拦截快速失败机制...');
  const cloudflareTestUrl = 'https://nowsecure.nl/'; // Cloudflare测试页
  try {
    const t0 = Date.now();
    const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: cloudflareTestUrl,
        targetCountry: 'US',
        proxyProviderURL: PROXY_URL,
        proxyPoolSize: 5,
        maxRetries: 1
      })
    });
    const visitData = await visitResp.json();
    const duration = Date.now() - t0;

    if (visitData.error?.type === 'antibot') {
      console.log(`✅ 快速失败成功 (${duration}ms) - 检测到风控:`, visitData.error.message);
    } else {
      console.log(`ℹ️  未触发风控 (${duration}ms) - 访问成功:`, visitData.result?.domain);
    }
  } catch (err) {
    console.error('❌ 测试失败:', err.message);
  }

  console.log('\n' + '='.repeat(80));
  console.log('测试完成');
  console.log('='.repeat(80));
}

testBrowserExec().catch(err => {
  console.error('测试执行失败:', err);
  process.exit(1);
});
