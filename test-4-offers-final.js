#!/usr/bin/env node

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

const OFFERS = [
  {
    name: 'pboost.me',
    url: 'https://pboost.me/ZDO2Bdek'
  },
  {
    name: 'dognet.com',
    url: 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F'
  },
  {
    name: 'yeahpromos.com',
    url: 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url='
  },
  {
    name: 'bonusarrive.com',
    url: 'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink'
  }
];

/**
 * 判断页面类型
 */
function classifyPageType(result) {
  if (!result) return 'unknown';

  const finalUrl = result.finalUrl?.toLowerCase() || '';
  const domain = result.domain?.toLowerCase() || '';

  // 1. 失效页/错误页
  const expiredKeywords = ['error', 'suspended', 'expired', 'not-found', '404', 'unavailable', 'invalid'];
  if (expiredKeywords.some(k => finalUrl.includes(k) || domain.includes(k))) {
    return 'expired';
  }

  // 2. 被拦截页（Cloudflare, 反机器人）
  if (!result.antiDetectionResult?.passed) {
    return 'blocked';
  }

  // 3. 中间页（affiliate network跳转页）
  if (result.isIntermediatePage) {
    return 'intermediate';
  }

  // 4. 最终落地页（商家网站）
  if (result.available && result.brandName && result.statusCode === 200) {
    return 'landing';
  }

  return 'unknown';
}

/**
 * 获取页面类型的描述
 */
function getPageTypeDescription(type) {
  const descriptions = {
    'landing': '✅ 最终落地页',
    'expired': '❌ 失效页/错误页',
    'blocked': '⚠️  被拦截页面',
    'intermediate': '⏸️  中间跳转页',
    'unknown': '❓ 未知类型'
  };
  return descriptions[type] || descriptions.unknown;
}

/**
 * 测试单个Offer
 */
async function testOffer(offer) {
  const startTime = Date.now();

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: offer.url,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 10,  // 增加到10，确保并发时有足够的代理可选
        maxRetries: 1
      })
    });

    const data = await response.json();
    const totalTime = Date.now() - startTime;

    const pageType = classifyPageType(data.result);
    const result = data.result || {};

    return {
      name: offer.name,
      url: offer.url,
      success: data.success,
      pageType,
      pageTypeDesc: getPageTypeDescription(pageType),
      finalUrl: result.finalUrl,
      domain: result.domain,
      brandName: result.brandName,
      statusCode: result.statusCode,
      redirectCount: result.redirectChain?.length || 0,
      antiDetectionPassed: result.antiDetectionResult?.passed,
      blockedBy: result.antiDetectionResult?.blockedBy,
      totalTime,
      navigationTime: data.timings?.navigationMs,
      stabilizationTime: data.timings?.stabilizationMs,
      error: data.error,
      redirectChain: result.redirectChain
    };
  } catch (error) {
    return {
      name: offer.name,
      url: offer.url,
      success: false,
      pageType: 'error',
      pageTypeDesc: '❌ 请求失败',
      totalTime: Date.now() - startTime,
      error: { message: error.message }
    };
  }
}

/**
 * 主测试函数
 */
async function main() {
  console.log('='.repeat(100));
  console.log('广告联盟 Offer URL 最终访问测试（并发）');
  console.log('='.repeat(100));
  console.log(`测试环境: browser-exec-preview`);
  console.log(`测试时间: ${new Date().toISOString()}`);
  console.log(`测试数量: ${OFFERS.length} 个 Offer`);
  console.log('');

  // 并发测试所有Offer
  console.log('🚀 开始并发测试...\n');
  const results = await Promise.all(OFFERS.map(offer => testOffer(offer)));

  // 输出结果
  console.log('\n' + '='.repeat(100));
  console.log('测试结果汇总');
  console.log('='.repeat(100));
  console.log('');

  results.forEach((result, index) => {
    console.log(`【${index + 1}/${results.length}】 ${result.name}`);
    console.log('─'.repeat(100));
    console.log(`  原始URL: ${result.url.slice(0, 80)}${result.url.length > 80 ? '...' : ''}`);
    console.log(`  成功状态: ${result.success ? '✅ 成功' : '❌ 失败'}`);
    console.log(`  页面类型: ${result.pageTypeDesc}`);
    console.log('');

    if (result.success) {
      console.log(`  最终URL: ${result.finalUrl}`);
      console.log(`  域名: ${result.domain}`);
      console.log(`  品牌: ${result.brandName || 'N/A'}`);
      console.log(`  状态码: ${result.statusCode || 'N/A'}`);
      console.log(`  重定向次数: ${result.redirectCount}`);
      console.log('');

      console.log(`  性能指标:`);
      console.log(`    总耗时: ${result.totalTime}ms`);
      console.log(`    导航耗时: ${result.navigationTime}ms`);
      console.log(`    稳定化耗时: ${result.stabilizationTime}ms`);
      console.log('');

      console.log(`  反检测:`);
      console.log(`    通过: ${result.antiDetectionPassed ? '✅ 是' : '❌ 否'}`);
      if (result.blockedBy) {
        console.log(`    拦截方: ${result.blockedBy}`);
      }

      if (result.redirectChain && result.redirectChain.length > 0) {
        console.log('');
        console.log(`  重定向链路 (${result.redirectChain.length}跳):`);
        result.redirectChain.forEach((step, idx) => {
          const url = typeof step === 'string' ? step : step.url;
          try {
            const urlObj = new URL(url);
            console.log(`    ${idx + 1}. ${urlObj.hostname}${urlObj.pathname.slice(0, 40)}`);
          } catch {
            console.log(`    ${idx + 1}. ${url.slice(0, 60)}`);
          }
        });
      }
    } else {
      console.log(`  错误类型: ${result.error?.type || 'unknown'}`);
      console.log(`  错误信息: ${result.error?.message || 'N/A'}`);
      console.log(`  耗时: ${result.totalTime}ms`);
    }

    console.log('');
  });

  // 统计分析
  console.log('='.repeat(100));
  console.log('统计分析');
  console.log('='.repeat(100));
  console.log('');

  const successCount = results.filter(r => r.success).length;
  const landingCount = results.filter(r => r.pageType === 'landing').length;
  const expiredCount = results.filter(r => r.pageType === 'expired').length;
  const blockedCount = results.filter(r => r.pageType === 'blocked').length;
  const intermediateCount = results.filter(r => r.pageType === 'intermediate').length;

  const avgTotalTime = Math.round(results.reduce((sum, r) => sum + r.totalTime, 0) / results.length);
  const successResults = results.filter(r => r.success && r.navigationTime);
  const avgNavigationTime = successResults.length > 0
    ? Math.round(successResults.reduce((sum, r) => sum + r.navigationTime, 0) / successResults.length)
    : 0;
  const avgStabilizationTime = successResults.length > 0
    ? Math.round(successResults.reduce((sum, r) => sum + r.stabilizationTime, 0) / successResults.length)
    : 0;

  console.log(`访问成功率: ${successCount}/${results.length} (${(successCount / results.length * 100).toFixed(1)}%)`);
  console.log('');
  console.log(`页面类型分布:`);
  console.log(`  ✅ 最终落地页: ${landingCount} (${(landingCount / results.length * 100).toFixed(1)}%)`);
  console.log(`  ❌ 失效页: ${expiredCount} (${(expiredCount / results.length * 100).toFixed(1)}%)`);
  console.log(`  ⚠️  被拦截: ${blockedCount} (${(blockedCount / results.length * 100).toFixed(1)}%)`);
  console.log(`  ⏸️  中间页: ${intermediateCount} (${(intermediateCount / results.length * 100).toFixed(1)}%)`);
  console.log('');
  console.log(`性能指标（平均）:`);
  console.log(`  总耗时: ${avgTotalTime}ms`);
  if (avgNavigationTime > 0) {
    console.log(`  导航耗时: ${avgNavigationTime}ms`);
    console.log(`  稳定化耗时: ${avgStabilizationTime}ms`);
  }
  console.log('');
  console.log('='.repeat(100));

  // 按页面类型分组展示
  console.log('\n按页面类型分组:');
  console.log('─'.repeat(100));

  const grouped = {
    landing: results.filter(r => r.pageType === 'landing'),
    expired: results.filter(r => r.pageType === 'expired'),
    blocked: results.filter(r => r.pageType === 'blocked'),
    intermediate: results.filter(r => r.pageType === 'intermediate'),
    error: results.filter(r => r.pageType === 'error' || r.pageType === 'unknown')
  };

  Object.entries(grouped).forEach(([type, items]) => {
    if (items.length > 0) {
      console.log(`\n${getPageTypeDescription(type)} (${items.length}):`);
      items.forEach(item => {
        console.log(`  - ${item.name}: ${item.finalUrl || item.url} (${item.totalTime}ms)`);
      });
    }
  });

  console.log('\n' + '='.repeat(100));
  console.log('测试完成');
  console.log('='.repeat(100));
}

main().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
