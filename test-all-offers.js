#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=15&type=-res-&proxyType=http&responseType=txt';

const TEST_OFFERS = [
  {
    name: 'pboost.me',
    url: 'https://pboost.me/ZDO2Bdek',
    network: 'pboost'
  },
  {
    name: 'dognet.com',
    url: 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
    network: 'dognet'
  },
  {
    name: 'yeahpromos.com',
    url: 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
    network: 'yeahpromos'
  },
  {
    name: 'bonusarrive.com',
    url: 'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink',
    network: 'bonusarrive'
  }
];

async function testOfferURL(offer, index, total) {
  console.log(`\n${'='.repeat(80)}`);
  console.log(`[${index}/${total}] 测试 ${offer.name}`);
  console.log(`${'='.repeat(80)}`);
  console.log(`URL: ${offer.url}`);

  const result = {
    offer: offer.name,
    network: offer.network,
    success: false,
    reachedFinalPage: false,
    stuckAtIntermediate: false,
    error: null,
    timings: {},
    finalInfo: {}
  };

  try {
    const t0 = Date.now();

    const visitResp = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: offer.url,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL,
        proxyPoolSize: 15,
        maxRetries: 1
      })
    });

    const visitData = await visitResp.json();
    const totalDuration = Date.now() - t0;

    result.timings = {
      total: totalDuration,
      navigation: visitData.timings?.navigationMs,
      stabilization: visitData.timings?.stabilizationMs,
      service: visitData.timings?.totalMs,
      redirectCount: visitData.timings?.redirectCount
    };

    if (visitData.success) {
      result.success = true;

      const res = visitData.result;
      result.finalInfo = {
        statusCode: res.statusCode,
        finalUrl: res.finalUrl,
        domain: res.domain,
        brandName: res.brandName,
        available: res.available,
        isIntermediatePage: res.isIntermediatePage,
        failureReason: res.failureReason
      };

      // 判断是否到达最终落地页
      if (res.available && !res.isIntermediatePage) {
        result.reachedFinalPage = true;
        console.log(`\n✅ 成功到达最终落地页`);
        console.log(`   域名: ${res.domain}`);
        console.log(`   品牌: ${res.brandName}`);
        console.log(`   最终URL: ${res.finalUrl}`);
        if (res.finalUrlSuffix) {
          console.log(`   参数: ${res.finalUrlSuffix}`);
        }
      } else if (res.isIntermediatePage) {
        result.stuckAtIntermediate = true;
        console.log(`\n⚠️  卡在中间页`);
        console.log(`   中间页域名: ${res.domain}`);
        console.log(`   失败原因: ${res.failureReason}`);
      } else {
        console.log(`\n⚠️  访问成功但页面不可用`);
        console.log(`   域名: ${res.domain}`);
        console.log(`   状态码: ${res.statusCode}`);
      }

      // 重定向链路
      if (res.redirectChain && res.redirectChain.length > 0) {
        console.log(`\n   重定向链路 (${res.redirectChain.length}跳):`);
        res.redirectChain.forEach((step, idx) => {
          const url = new URL(step.url || step);
          console.log(`   ${idx + 1}. ${url.hostname}`);
        });
      }

    } else {
      result.error = visitData.error?.message || 'Unknown error';
      console.log(`\n❌ 访问失败`);
      console.log(`   错误类型: ${visitData.error?.type}`);
      console.log(`   错误信息: ${visitData.error?.message?.slice(0, 150)}`);
    }

    // 耗时信息
    console.log(`\n⏱️  耗时统计:`);
    console.log(`   总耗时: ${totalDuration}ms`);
    if (visitData.timings) {
      console.log(`   - 导航: ${visitData.timings.navigationMs}ms`);
      console.log(`   - 稳定化: ${visitData.timings.stabilizationMs}ms`);
      console.log(`   - 重定向次数: ${visitData.timings.redirectCount || 0}`);
    }

    // 代理和Referer信息
    if (visitData.metadata) {
      console.log(`\n🔧 访问配置:`);
      console.log(`   代理IP: ${visitData.metadata.proxyServer?.split(':').slice(0, 2).join(':')}`);
      console.log(`   Referer: ${visitData.metadata.referer}`);
    }

  } catch (err) {
    result.error = err.message;
    console.log(`\n❌ 请求异常: ${err.message}`);
  }

  return result;
}

async function runTests() {
  console.log('╔' + '═'.repeat(78) + '╗');
  console.log('║' + ' '.repeat(20) + '广告联盟 Offer URL 测试报告' + ' '.repeat(28) + '║');
  console.log('╚' + '═'.repeat(78) + '╝');
  console.log(`\n测试环境: browser-exec-preview`);
  console.log(`测试时间: ${new Date().toLocaleString('zh-CN')}`);
  console.log(`Offer数量: ${TEST_OFFERS.length}`);

  const results = [];

  // 逐个测试
  for (let i = 0; i < TEST_OFFERS.length; i++) {
    const result = await testOfferURL(TEST_OFFERS[i], i + 1, TEST_OFFERS.length);
    results.push(result);

    // 等待2秒再测试下一个
    if (i < TEST_OFFERS.length - 1) {
      await new Promise(r => setTimeout(r, 2000));
    }
  }

  // 汇总统计
  console.log(`\n\n${'═'.repeat(80)}`);
  console.log('测试结果汇总');
  console.log('═'.repeat(80));

  const summary = {
    total: results.length,
    success: results.filter(r => r.success).length,
    reachedFinalPage: results.filter(r => r.reachedFinalPage).length,
    stuckAtIntermediate: results.filter(r => r.stuckAtIntermediate).length,
    failed: results.filter(r => !r.success).length
  };

  console.log(`\n📊 总体统计:`);
  console.log(`   总数: ${summary.total}`);
  console.log(`   访问成功: ${summary.success} (${(summary.success / summary.total * 100).toFixed(1)}%)`);
  console.log(`   到达落地页: ${summary.reachedFinalPage} (${(summary.reachedFinalPage / summary.total * 100).toFixed(1)}%)`);
  console.log(`   卡在中间页: ${summary.stuckAtIntermediate}`);
  console.log(`   访问失败: ${summary.failed}`);

  // 详细列表
  console.log(`\n📋 详细结果:`);
  console.log('┌' + '─'.repeat(78) + '┐');
  console.log(`│ ${'Offer'.padEnd(20)} │ ${'状态'.padEnd(10)} │ ${'域名'.padEnd(20)} │ ${'耗时'.padEnd(15)} │`);
  console.log('├' + '─'.repeat(78) + '┤');

  results.forEach(r => {
    let status = '❌ 失败';
    if (r.reachedFinalPage) status = '✅ 成功';
    else if (r.stuckAtIntermediate) status = '⚠️  中间页';
    else if (r.success) status = '⚠️  不可用';

    const domain = r.finalInfo.domain || 'N/A';
    const timing = r.timings.service ? `${r.timings.service}ms` : 'N/A';

    console.log(`│ ${r.offer.padEnd(20)} │ ${status.padEnd(10)} │ ${domain.padEnd(20)} │ ${timing.padEnd(15)} │`);
  });
  console.log('└' + '─'.repeat(78) + '┘');

  // 耗时分析
  const successTimings = results
    .filter(r => r.reachedFinalPage)
    .map(r => r.timings.service)
    .filter(t => t);

  if (successTimings.length > 0) {
    const avgTiming = successTimings.reduce((a, b) => a + b, 0) / successTimings.length;
    const minTiming = Math.min(...successTimings);
    const maxTiming = Math.max(...successTimings);

    console.log(`\n⏱️  耗时分析 (成功访问):`);
    console.log(`   平均: ${avgTiming.toFixed(0)}ms`);
    console.log(`   最快: ${minTiming}ms`);
    console.log(`   最慢: ${maxTiming}ms`);
  }

  // 最终结论
  console.log(`\n${'═'.repeat(80)}`);

  if (summary.reachedFinalPage === summary.total) {
    console.log('🎉 测试完全成功！所有Offer URL都能正常访问到最终落地页');
  } else if (summary.reachedFinalPage >= summary.total * 0.75) {
    console.log('✅ 测试基本成功！大部分Offer URL能正常访问到最终落地页');
  } else if (summary.reachedFinalPage > 0) {
    console.log('⚠️  测试部分成功，部分Offer URL存在问题');
  } else {
    console.log('❌ 测试失败，无法访问到最终落地页');
  }

  console.log('═'.repeat(80));

  // 问题Offer
  const problemOffers = results.filter(r => !r.reachedFinalPage);
  if (problemOffers.length > 0) {
    console.log(`\n⚠️  需要关注的 Offer (${problemOffers.length}个):`);
    problemOffers.forEach(r => {
      console.log(`\n   - ${r.offer}`);
      if (r.stuckAtIntermediate) {
        console.log(`     问题: 卡在中间页 (${r.finalInfo.domain})`);
        console.log(`     原因: ${r.finalInfo.failureReason}`);
      } else if (r.error) {
        console.log(`     问题: ${r.error.slice(0, 100)}`);
      } else {
        console.log(`     问题: 页面不可用`);
      }
    });
  }

  console.log('\n');
}

runTests().catch(err => {
  console.error('测试执行失败:', err);
  process.exit(1);
});
