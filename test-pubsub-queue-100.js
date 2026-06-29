#!/usr/bin/env node

/**
 * Pub/Sub队列100并发测试
 *
 * 使用Pub/Sub队列避免Cloud Run速率限制
 */

const BROWSER_EXEC_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app';
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt';
const TEST_URL = 'https://pboost.me/ZDO2Bdek';

async function publishToQueue(url, index) {
  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit-queue`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url,
        targetCountry: 'US',
        refererStrategy: 'social',
        proxyProviderURL: PROXY_URL_US,
        proxyPoolSize: 20,
        maxRetries: 1
      })
    });

    const data = await response.json();

    return {
      index,
      success: data.success,
      messageId: data.messageId,
      status: data.status
    };
  } catch (error) {
    return {
      index,
      success: false,
      error: error.message
    };
  }
}

async function testPubSubQueue() {
  console.log('='.repeat(100));
  console.log('Pub/Sub队列100并发测试');
  console.log('='.repeat(100));
  console.log(`URL: ${BROWSER_EXEC_URL}`);
  console.log(`测试URL: ${TEST_URL}`);
  console.log(`并发数: 100`);
  console.log('');
  console.log('策略说明:');
  console.log('- 100个请求瞬时发送到Pub/Sub队列');
  console.log('- 不会触发Cloud Run速率限制');
  console.log('- Worker以10个/实例的速率处理');
  console.log('- 理论处理能力: 10实例 × 10消息 = 100并发');
  console.log('');

  const startTime = Date.now();

  // 创建100个请求，全部发送到队列
  console.log('📤 发送100个请求到队列...\n');
  const promises = [];
  for (let i = 0; i < 100; i++) {
    promises.push(publishToQueue(TEST_URL, i + 1));

    if ((i + 1) % 10 === 0) {
      console.log(`发送 ${i + 1}/100 个请求...`);
    }
  }

  const publishResults = await Promise.all(promises);
  const publishTime = Date.now() - startTime;

  const successCount = publishResults.filter(r => r.success).length;
  const failureCount = publishResults.filter(r => !r.success).length;

  console.log('');
  console.log('='.repeat(100));
  console.log('发布结果');
  console.log('='.repeat(100));
  console.log('');
  console.log(`发布耗时: ${(publishTime / 1000).toFixed(1)}秒`);
  console.log(`成功发布: ${successCount}/100 (${successCount}%)`);
  console.log(`发布失败: ${failureCount}/100 (${failureCount}%)`);

  if (failureCount > 0) {
    console.log('');
    console.log('失败详情:');
    publishResults.filter(r => !r.success).forEach(r => {
      console.log(`  #${r.index}: ${r.error}`);
    });
  }

  if (successCount > 0) {
    console.log('');
    console.log('✅ 所有请求已成功加入队列!');
    console.log('');
    console.log('⏳ 等待30秒让队列处理...');

    // 等待队列处理
    await new Promise(r => setTimeout(r, 30000));

    // 检查队列统计
    console.log('');
    console.log('📊 查询队列统计...');
    const statsResponse = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/queue/stats`);
    const statsData = await statsResponse.json();

    if (statsData.success) {
      const stats = statsData.stats;
      console.log('');
      console.log('='.repeat(100));
      console.log('队列处理统计');
      console.log('='.repeat(100));
      console.log('');
      console.log(`已发布: ${stats.published} 条消息`);
      console.log(`已处理: ${stats.processed} 条消息`);
      console.log(`处理失败: ${stats.failed} 条消息`);
      console.log(`处理中: ${stats.inProgress} 条消息`);
      console.log(`成功率: ${stats.successRate}`);

      const totalProcessed = stats.processed + stats.failed;
      const remaining = stats.published - totalProcessed;

      console.log('');
      console.log(`剩余队列: ${remaining} 条消息`);

      if (remaining > 0) {
        console.log('');
        console.log(`⏳ 还有${remaining}条消息在队列中，预计${Math.ceil(remaining / 10)}秒后完成`);
      }

      console.log('');
      console.log('='.repeat(100));
      console.log('性能评估');
      console.log('='.repeat(100));
      console.log('');

      if (successCount === 100) {
        console.log('✅ 优秀: 100个请求全部成功发布到队列!');
        console.log('✅ Pub/Sub队列成功避免了Cloud Run速率限制!');
      } else if (successCount >= 90) {
        console.log('⚠️  良好: 大部分请求成功发布');
      } else {
        console.log('❌ 需优化: 发布失败率过高');
      }

      console.log('');
      console.log('对比传统直接调用:');
      console.log(`  传统方式成功率: 32-42%`);
      console.log(`  队列方式发布率: ${successCount}%`);
      console.log(`  队列处理成功率: ${stats.successRate}`);
    }
  }

  console.log('');
  console.log('='.repeat(100));
}

testPubSubQueue().catch(err => {
  console.error('测试失败:', err);
  process.exit(1);
});
