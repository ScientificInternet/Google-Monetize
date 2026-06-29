/**
 * browser-exec 服务 Pub/Sub 队列测试脚本
 * 测试 4 个广告联盟 Offer URL (使用队列方式)
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-yt54xvsg5q-an.a.run.app';
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
 * 发布任务到 Pub/Sub 队列
 */
async function publishToQueue(testCase) {
  console.log(`\n📤 发布任务到队列: ${testCase.name}`);

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit-queue`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        url: testCase.url,
        targetCountry: 'US',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 5,
        maxRetries: 1,
      }),
    });

    const data = await response.json();

    if (data.success) {
      console.log(`   ✅ 成功 - Message ID: ${data.messageId}`);
      console.log(`   状态: ${data.status || 'queued'}`);
    } else {
      console.log(`   ❌ 失败 - ${data.error || 'Unknown error'}`);
    }

    return {
      name: testCase.name,
      url: testCase.url,
      success: data.success,
      messageId: data.messageId,
      error: data.error,
    };
  } catch (error) {
    console.log(`   ❌ 请求失败 - ${error.message}`);
    return {
      name: testCase.name,
      url: testCase.url,
      success: false,
      error: error.message,
    };
  }
}

/**
 * 查询队列统计
 */
async function getQueueStats() {
  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/queue/stats`);
    const data = await response.json();

    if (data.success) {
      return data.stats;
    }
    return null;
  } catch (error) {
    console.error(`查询队列统计失败: ${error.message}`);
    return null;
  }
}

/**
 * 等待队列处理完成
 */
async function waitForQueueProcessing(expectedCount, maxWaitTime = 180000) {
  console.log(`\n⏳ 等待队列处理 ${expectedCount} 个任务 (最大等待 ${maxWaitTime / 1000}s)...`);

  const startTime = Date.now();
  let lastStats = null;

  while (Date.now() - startTime < maxWaitTime) {
    const stats = await getQueueStats();

    if (stats) {
      const totalProcessed = stats.processed + stats.failed;
      const progress = ((totalProcessed / expectedCount) * 100).toFixed(1);

      // 只在统计发生变化时输出
      if (
        !lastStats ||
        lastStats.processed !== stats.processed ||
        lastStats.failed !== stats.failed
      ) {
        console.log(
          `   进度: ${totalProcessed}/${expectedCount} (${progress}%) - ` +
            `成功: ${stats.processed}, 失败: ${stats.failed}, 处理中: ${stats.inProgress}`
        );
        lastStats = stats;
      }

      // 检查是否完成
      if (totalProcessed >= expectedCount) {
        console.log(`\n✅ 队列处理完成！`);
        return stats;
      }
    }

    // 每 5 秒查询一次
    await new Promise(resolve => setTimeout(resolve, 5000));
  }

  console.log(`\n⚠️ 等待超时，返回当前统计`);
  return lastStats;
}

/**
 * 运行 Pub/Sub 队列测试
 */
async function runPubSubTest() {
  console.log('=====================================');
  console.log('browser-exec Pub/Sub 队列测试');
  console.log('=====================================');
  console.log(`测试服务: ${BROWSER_EXEC_URL}`);
  console.log(`代理服务: ${PROXY_URL_US.substring(0, 50)}...`);
  console.log(`测试数量: ${TEST_URLS.length}`);
  console.log('=====================================\n');

  // Step 1: 发布所有任务到队列
  console.log('【第一步】发布任务到 Pub/Sub 队列');
  console.log('=====================================');

  const publishStartTime = Date.now();
  const publishResults = [];

  for (const testCase of TEST_URLS) {
    const result = await publishToQueue(testCase);
    publishResults.push(result);

    // 发布间隔 500ms
    await new Promise(resolve => setTimeout(resolve, 500));
  }

  const publishDuration = ((Date.now() - publishStartTime) / 1000).toFixed(2);
  const successCount = publishResults.filter(r => r.success).length;
  const failureCount = publishResults.filter(r => !r.success).length;

  console.log(`\n📊 发布结果:`);
  console.log(`   耗时: ${publishDuration}s`);
  console.log(`   成功: ${successCount}/${TEST_URLS.length}`);
  console.log(`   失败: ${failureCount}/${TEST_URLS.length}`);

  if (failureCount > 0) {
    console.log(`\n   失败详情:`);
    publishResults
      .filter(r => !r.success)
      .forEach(r => {
        console.log(`     - ${r.name}: ${r.error}`);
      });
  }

  if (successCount === 0) {
    console.log(`\n❌ 所有任务发布失败，终止测试`);
    return;
  }

  // Step 2: 等待队列处理完成
  console.log('\n\n【第二步】等待队列处理');
  console.log('=====================================');

  const finalStats = await waitForQueueProcessing(successCount, 180000);

  if (!finalStats) {
    console.log(`\n❌ 无法获取队列统计`);
    return;
  }

  // Step 3: 输出最终报告
  console.log('\n\n【第三步】测试结果汇总');
  console.log('=====================================');

  const totalDuration = ((Date.now() - publishStartTime) / 1000).toFixed(2);

  console.log(`\n📊 总体统计:`);
  console.log(`   总耗时: ${totalDuration}s`);
  console.log(`   发布成功: ${successCount}/${TEST_URLS.length} (${((successCount / TEST_URLS.length) * 100).toFixed(1)}%)`);
  console.log(`   队列处理成功: ${finalStats.processed}/${successCount} (${finalStats.successRate})`);
  console.log(`   队列处理失败: ${finalStats.failed}/${successCount}`);

  console.log(`\n📈 性能指标:`);
  console.log(`   发布耗时: ${publishDuration}s`);
  console.log(`   平均发布耗时: ${(parseFloat(publishDuration) / successCount).toFixed(2)}s/任务`);
  console.log(`   队列处理耗时: ${(parseFloat(totalDuration) - parseFloat(publishDuration)).toFixed(2)}s`);

  console.log(`\n📋 队列统计:`);
  console.log(`   已发布: ${finalStats.published} 条消息`);
  console.log(`   已处理: ${finalStats.processed} 条消息`);
  console.log(`   处理失败: ${finalStats.failed} 条消息`);
  console.log(`   处理中: ${finalStats.inProgress} 条消息`);

  console.log(`\n✅ 队列方式优势:`);
  console.log(`   1. 瞬时发布: ${publishDuration}s 完成 ${successCount} 个任务提交`);
  console.log(`   2. 避免限流: 不受 Cloud Run 并发限制影响`);
  console.log(`   3. 异步处理: 提交即返回，后台自动处理`);
  console.log(`   4. 容错性强: 失败任务自动重试`);

  console.log('\n=====================================\n');

  return {
    publishDuration: parseFloat(publishDuration),
    totalDuration: parseFloat(totalDuration),
    publishSuccess: successCount,
    publishFailure: failureCount,
    queueStats: finalStats,
  };
}

// 执行测试
runPubSubTest()
  .then(results => {
    console.log('测试完成');
    process.exit(0);
  })
  .catch(error => {
    console.error('测试失败:', error);
    process.exit(1);
  });
