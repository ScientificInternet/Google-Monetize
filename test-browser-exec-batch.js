/**
 * browser-exec 服务批量测试脚本
 * 测试 4 个广告联盟 Offer URL
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-yt54xvsg5q-an.a.run.app/api/v1/browser/visit';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';

// 测试 URL 列表
const TEST_URLS = [
  {
    name: 'pboost.me',
    url: 'https://pboost.me/ZDO2Bdek',
  },
  {
    name: 'dognet.com',
    url: 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
  },
  {
    name: 'yeahpromos.com',
    url: 'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
  },
  {
    name: 'bonusarrive.com',
    url: 'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink',
  },
];

/**
 * 访问单个 URL
 */
async function visitUrl(testCase) {
  const startTime = Date.now();
  console.log(`\n======================================`);
  console.log(`开始测试: ${testCase.name}`);
  console.log(`URL: ${testCase.url}`);
  console.log(`======================================`);

  try {
    const response = await fetch(BROWSER_EXEC_URL, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        url: testCase.url,
        targetCountry: 'US',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 5,
      }),
    });

    const data = await response.json();
    const duration = ((Date.now() - startTime) / 1000).toFixed(2);

    console.log(`\n✅ 测试完成 (耗时: ${duration}s)`);
    console.log(`HTTP 状态: ${response.status}`);

    if (data.result) {
      const result = data.result;
      console.log(`\n【最终结果】`);
      console.log(`  - 最终 URL: ${result.finalUrl || 'N/A'}`);
      console.log(`  - 是否可用: ${result.available ? '✅ 是' : '❌ 否'}`);
      console.log(`  - HTTP 状态: ${result.httpStatus || 'N/A'}`);
      console.log(`  - 品牌名称: ${result.brandName || 'N/A'}`);

      if (result.isIntermediatePage) {
        console.log(`  - 中间页: ${result.isIntermediatePage.isIntermediate ? '是' : '否'}`);
        if (result.isIntermediatePage.isIntermediate) {
          console.log(`    - 类型: ${result.isIntermediatePage.subtype || 'N/A'}`);
          console.log(`    - 置信度: ${result.isIntermediatePage.confidence || 'N/A'}`);
          console.log(`    - 匹配模式: ${result.isIntermediatePage.matchedPatternId || 'N/A'}`);
        }
      }

      if (result.redirectChain && result.redirectChain.length > 0) {
        console.log(`\n【跳转链】(共 ${result.redirectChain.length} 跳)`);
        result.redirectChain.forEach((redirect, index) => {
          console.log(`  ${index + 1}. ${redirect.url}`);
        });
      }

      if (result.failureReason) {
        console.log(`\n【失败原因】`);
        console.log(`  ${result.failureReason}`);
      }

      if (result.metadata) {
        console.log(`\n【元数据】`);
        console.log(`  - 标题: ${result.metadata.title || 'N/A'}`);
        console.log(`  - 描述: ${result.metadata.description ? result.metadata.description.substring(0, 100) + '...' : 'N/A'}`);
      }
    } else if (data.error) {
      console.log(`\n❌ 访问失败`);
      console.log(`错误: ${data.error}`);
    }

    return {
      name: testCase.name,
      url: testCase.url,
      duration,
      success: response.status === 200,
      result: data.result || null,
      error: data.error || null,
    };
  } catch (error) {
    const duration = ((Date.now() - startTime) / 1000).toFixed(2);
    console.log(`\n❌ 请求失败 (耗时: ${duration}s)`);
    console.log(`错误: ${error.message}`);

    return {
      name: testCase.name,
      url: testCase.url,
      duration,
      success: false,
      result: null,
      error: error.message,
    };
  }
}

/**
 * 批量测试所有 URL (顺序执行)
 */
async function runTests() {
  console.log('=====================================');
  console.log('browser-exec 批量测试');
  console.log('=====================================');
  console.log(`测试服务: ${BROWSER_EXEC_URL}`);
  console.log(`代理服务: ${PROXY_URL_US.substring(0, 50)}...`);
  console.log(`测试数量: ${TEST_URLS.length}`);
  console.log('=====================================\n');

  const results = [];

  for (const testCase of TEST_URLS) {
    const result = await visitUrl(testCase);
    results.push(result);

    // 测试间隔 2 秒,避免过载
    if (TEST_URLS.indexOf(testCase) < TEST_URLS.length - 1) {
      console.log('\n等待 2 秒后继续下一个测试...\n');
      await new Promise(resolve => setTimeout(resolve, 2000));
    }
  }

  // 打印汇总报告
  console.log('\n\n');
  console.log('=====================================');
  console.log('测试汇总报告');
  console.log('=====================================');

  const successCount = results.filter(r => r.success).length;
  const totalTime = results.reduce((sum, r) => sum + parseFloat(r.duration), 0).toFixed(2);
  const avgTime = (totalTime / results.length).toFixed(2);

  console.log(`\n【总体统计】`);
  console.log(`  - 成功数: ${successCount}/${results.length}`);
  console.log(`  - 成功率: ${((successCount / results.length) * 100).toFixed(2)}%`);
  console.log(`  - 总耗时: ${totalTime}s`);
  console.log(`  - 平均耗时: ${avgTime}s`);

  console.log(`\n【详细结果】`);
  results.forEach((result, index) => {
    const status = result.success ? '✅' : '❌';
    const available = result.result?.available ? '可用' : '不可用/失效';
    const isIntermediate = result.result?.isIntermediatePage?.isIntermediate ? '(中间页)' : '';

    console.log(`\n${index + 1}. ${status} ${result.name} - ${result.duration}s`);
    console.log(`   URL: ${result.url}`);

    if (result.success && result.result) {
      console.log(`   最终: ${result.result.finalUrl || 'N/A'}`);
      console.log(`   状态: ${available} ${isIntermediate}`);
      if (result.result.brandName) {
        console.log(`   品牌: ${result.result.brandName}`);
      }
      if (result.result.failureReason) {
        console.log(`   原因: ${result.result.failureReason}`);
      }
      if (result.result.redirectChain) {
        console.log(`   跳转: ${result.result.redirectChain.length} 跳`);
      }
    } else {
      console.log(`   错误: ${result.error || 'Unknown error'}`);
    }
  });

  console.log('\n=====================================\n');

  return results;
}

// 执行测试
runTests()
  .then(results => {
    console.log('所有测试完成');
    process.exit(0);
  })
  .catch(error => {
    console.error('测试执行失败:', error);
    process.exit(1);
  });
