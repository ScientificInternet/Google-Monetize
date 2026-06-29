#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=10&type=-res-&proxyType=http&responseType=txt';

const PROBLEM_OFFERS = [
  {
    name: 'yeahpromos.com',
    url: 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
    issue: '卡在dailybacks.com中间页'
  },
  {
    name: 'bonusarrive.com',
    url: 'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink',
    issue: 'Cloudflare拦截'
  }
];

async function retestOffer(offer, config = {}) {
  console.log(`\n${'='.repeat(80)}`);
  console.log(`重新测试: ${offer.name}`);
  console.log(`原问题: ${offer.issue}`);
  console.log(`配置: ${JSON.stringify(config)}`);
  console.log('='.repeat(80));

  try {
    const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: offer.url,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL,
        proxyPoolSize: 10,
        maxRetries: config.maxRetries || 1,
        advancedOptions: {
          stabilizeMs: config.stabilizeMs || 15000
        }
      })
    });

    const visitData = await visitResp.json();

    if (visitData.success && visitData.result?.available && !visitData.result?.isIntermediatePage) {
      console.log(`\n✅ 成功到达最终落地页！`);
      console.log(`   域名: ${visitData.result.domain}`);
      console.log(`   品牌: ${visitData.result.brandName}`);
      console.log(`   最终URL: ${visitData.result.finalUrl}`);
      console.log(`   耗时: ${visitData.timings.totalMs}ms`);
      return { success: true, data: visitData };
    } else if (visitData.result?.isIntermediatePage) {
      console.log(`\n⚠️  仍然卡在中间页: ${visitData.result.domain}`);
      console.log(`   失败原因: ${visitData.result.failureReason}`);
      return { success: false, intermediate: true, data: visitData };
    } else if (!visitData.success) {
      console.log(`\n❌ 访问失败`);
      console.log(`   错误: ${visitData.error?.message?.slice(0, 150)}`);
      return { success: false, error: visitData.error, data: visitData };
    } else {
      console.log(`\n⚠️  页面不可用`);
      return { success: false, data: visitData };
    }
  } catch (err) {
    console.log(`\n❌ 请求异常: ${err.message}`);
    return { success: false, error: err.message };
  }
}

async function runRetests() {
  console.log('╔' + '═'.repeat(78) + '╗');
  console.log('║' + ' '.repeat(25) + '问题 Offer 重测报告' + ' '.repeat(32) + '║');
  console.log('╚' + '═'.repeat(78) + '╝\n');

  const results = [];

  // 测试 yeahpromos.com - 增加稳定化时间
  console.log('\n[策略1] yeahpromos.com - 增加稳定化时间到30秒');
  let result1 = await retestOffer(PROBLEM_OFFERS[0], { stabilizeMs: 30000 });
  results.push({ offer: PROBLEM_OFFERS[0].name, strategy: '增加稳定化时间', ...result1 });

  await new Promise(r => setTimeout(r, 3000));

  // 测试 bonusarrive.com - 重试机制
  console.log('\n[策略2] bonusarrive.com - 启用重试机制');
  let result2 = await retestOffer(PROBLEM_OFFERS[1], { maxRetries: 2, stabilizeMs: 20000 });
  results.push({ offer: PROBLEM_OFFERS[1].name, strategy: '启用重试', ...result2 });

  // 汇总
  console.log(`\n\n${'═'.repeat(80)}`);
  console.log('重测结果汇总');
  console.log('═'.repeat(80));

  results.forEach(r => {
    const status = r.success ? '✅ 成功' : (r.intermediate ? '⚠️  中间页' : '❌ 失败');
    console.log(`\n${r.offer} (${r.strategy}): ${status}`);
    if (r.success && r.data?.result) {
      console.log(`  - 落地页域名: ${r.data.result.domain}`);
      console.log(`  - 耗时: ${r.data.timings?.totalMs}ms`);
    }
  });

  const successCount = results.filter(r => r.success).length;
  console.log(`\n成功率: ${successCount}/${results.length} (${(successCount / results.length * 100).toFixed(1)}%)`);

  console.log('\n' + '═'.repeat(80));
}

runRetests().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
