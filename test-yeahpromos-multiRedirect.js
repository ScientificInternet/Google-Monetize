#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=';

async function testMultiRedirect() {
  console.log('='.repeat(80));
  console.log('测试 yeahpromos.com 多重重定向追踪');
  console.log('='.repeat(80));
  console.log(`预期跳转链路:`);
  console.log(`  1. yeahpromos.com`);
  console.log(`  2. dailybacks.com/return.html?id=error_suspended.html`);
  console.log(`  3. dailybacks.com/error_suspended.html (最终失效页)`);
  console.log('');

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

    console.log(`\n${'='.repeat(80)}`);
    console.log('测试结果');
    console.log('='.repeat(80));

    console.log(`\n✅ 基本信息:`);
    console.log(`  访问成功: ${visitData.success}`);
    console.log(`  总耗时: ${totalTime}ms`);

    if (visitData.success) {
      const result = visitData.result || {};

      console.log(`\n📍 最终页面:`);
      console.log(`  最终URL: ${result.finalUrl}`);
      console.log(`  域名: ${result.domain}`);
      console.log(`  品牌: ${result.brandName}`);
      console.log(`  状态码: ${result.statusCode}`);
      console.log(`  是否中间页: ${result.isIntermediatePage ? '❌ 是' : '✅ 否'}`);
      console.log(`  可用性: ${result.available ? '✅ 可用' : '❌ 不可用'}`);
      console.log(`  失败原因: ${result.failureReason || 'N/A'}`);

      if (result.redirectChain && result.redirectChain.length > 0) {
        console.log(`\n🔀 重定向链路 (${result.redirectChain.length}跳):`);
        result.redirectChain.forEach((step, idx) => {
          const url = typeof step === 'string' ? step : step.url;
          const urlObj = new URL(url);
          console.log(`  ${idx + 1}. ${urlObj.hostname}${urlObj.pathname}`);
          if (urlObj.search) {
            console.log(`     查询参数: ${urlObj.search.slice(0, 100)}${urlObj.search.length > 100 ? '...' : ''}`);
          }
        });
      }

      console.log(`\n⏱️  性能统计:`);
      if (visitData.timings) {
        console.log(`  导航耗时: ${visitData.timings.navigationMs}ms`);
        console.log(`  稳定化耗时: ${visitData.timings.stabilizationMs}ms`);
        console.log(`  重定向次数: ${visitData.timings.redirectCount || 0}`);
        console.log(`  服务总耗时: ${visitData.timings.totalMs}ms`);
      }

      console.log(`\n🔧 访问配置:`);
      if (visitData.metadata) {
        console.log(`  代理IP: ${visitData.metadata.proxyServer?.split(':').slice(0, 2).join(':') || 'N/A'}`);
        console.log(`  Referer: ${visitData.metadata.referer || 'N/A'}`);
        console.log(`  代理已启用: ${visitData.metadata.proxyUsed ? '✅ 是' : '❌ 否'}`);
      }

      // 验证结果
      console.log(`\n${'='.repeat(80)}`);
      console.log('验证结果');
      console.log('='.repeat(80));

      const finalDomain = result.domain || '';
      const finalPath = result.finalUrl || '';

      if (finalDomain.includes('dailybacks') && finalPath.includes('error_suspended')) {
        console.log(`✅ 成功: 到达最终失效页 (dailybacks.com/error_suspended.html)`);
        console.log(`✅ 多重重定向追踪正常工作`);
        console.log(`✅ 正确识别为失效页面 (available: ${result.available})`);

        if (!result.isIntermediatePage) {
          console.log(`✅ 正确识别为最终页面 (非中间页)`);
        } else {
          console.log(`❌ 错误: 仍被识别为中间页`);
        }
      } else if (finalDomain.includes('dailybacks') && finalPath.includes('return')) {
        console.log(`❌ 失败: 卡在中间跳转页 (dailybacks.com/return.html)`);
        console.log(`   说明稳定化逻辑未能追踪到最终的 error_suspended.html`);
      } else {
        console.log(`⚠️  未知结果: ${finalDomain}${finalPath}`);
      }

    } else {
      console.log(`\n❌ 访问失败:`);
      console.log(`  错误类型: ${visitData.error?.type}`);
      console.log(`  错误信息: ${visitData.error?.message}`);
    }

  } catch (err) {
    console.log(`\n❌ 请求异常: ${err.message}`);
  }

  console.log('\n' + '='.repeat(80));
}

testMultiRedirect().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
