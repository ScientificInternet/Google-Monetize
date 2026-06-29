/**
 * 测试 browser-exec 预发环境 - 广告联盟 Offer URL 访问测试
 *
 * 使用 Pub/Sub 队列批量测试4个 Offer URL，检测是否能访问到最终落地页
 */

import { PubSub } from '@google-cloud/pubsub'

const pubsub = new PubSub({
  projectId: '${GCP_PROJECT_ID}'
})

const TOPIC_NAME = 'browser-visit-requests'
const SUBSCRIPTION_NAME = 'browser-visit-workers'

// 测试的4个 Offer URL
const OFFER_URLS = [
  "https://pboost.me/ZDO2Bdek",
  "https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F",
  "https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=",
  "https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink"
]

// 美国代理配置
const PROXY_URL = process.env.PROXY_URL_US || "https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt"

async function publishTestRequests() {
  const topic = pubsub.topic(TOPIC_NAME)
  const results = []

  console.log('📤 发布测试请求到 Pub/Sub 队列...\n')

  for (const url of OFFER_URLS) {
    try {
      const request = {
        url,
        targetCountry: 'US',
        proxyProviderURL: PROXY_URL,
        proxyPoolSize: 10,
        refererStrategy: 'social',
        maxRetries: 3,
        publishedAt: Date.now()
      }

      const messageId = await topic.publishMessage({
        json: request
      })

      results.push({
        url,
        messageId,
        status: 'published'
      })

      console.log(`✅ [${messageId.substring(0, 8)}...] ${url}`)
    } catch (error) {
      results.push({
        url,
        error: error.message,
        status: 'failed'
      })
      console.error(`❌ 发布失败: ${url} - ${error.message}`)
    }
  }

  console.log(`\n📊 发布完成: ${results.filter(r => r.status === 'published').length}/${OFFER_URLS.length} 成功\n`)
  return results
}

async function monitorResults() {
  const subscription = pubsub.subscription(SUBSCRIPTION_NAME)
  const processedResults = []
  const timeout = 180000 // 3分钟超时

  console.log('👀 监控处理结果...\n')

  return new Promise((resolve) => {
    const startTime = Date.now()
    const timer = setInterval(() => {
      if (Date.now() - startTime > timeout || processedResults.length >= OFFER_URLS.length) {
        clearInterval(timer)
        subscription.close()
        resolve(processedResults)
      }
    }, 1000)

    subscription.on('message', async (message) => {
      try {
        const request = JSON.parse(message.data.toString())
        const result = {
          url: request.url,
          requestTime: request.publishedAt,
          responseTime: Date.now(),
          latency: Date.now() - request.publishedAt
        }

        processedResults.push(result)
        console.log(`📥 [${processedResults.length}/${OFFER_URLS.length}] 收到结果: ${request.url}`)

        // 不 ack，让 worker 处理
        message.nack()

      } catch (error) {
        console.error('解析消息失败:', error.message)
        message.nack()
      }
    })

    subscription.on('error', (error) => {
      console.error('订阅错误:', error.message)
    })
  })
}

async function checkWorkerResults() {
  // 等待 worker 处理完成
  await new Promise(r => setTimeout(r, 5000))

  console.log('\n🔍 检查 Cloud Run 日志获取处理结果...\n')

  const command = `gcloud run services logs read browser-exec --region=asia-northeast1 --limit=100 --format="value(textPayload)" --project=${GCP_PROJECT_ID} | grep -E "(Final URL|Brand|Available|Failed)" | tail -20`

  const { execSync } = await import('child_process')
  try {
    const output = execSync(command, { encoding: 'utf8' })
    console.log('最新处理日志:')
    console.log(output)
  } catch (error) {
    console.error('无法获取日志:', error.message)
  }
}

async function main() {
  console.log('🚀 browser-exec Offer URL 测试开始\n')
  console.log('测试 URL 列表:')
  OFFER_URLS.forEach((url, i) => console.log(`  ${i + 1}. ${url}`))
  console.log('\n' + '='.repeat(80) + '\n')

  // 1. 发布请求到队列
  const published = await publishTestRequests()

  // 2. 等待处理
  console.log('⏳ 等待 worker 处理 (最多3分钟)...\n')
  await new Promise(r => setTimeout(r, 30000)) // 等待30秒让 worker 处理

  // 3. 查看结果
  await checkWorkerResults()

  console.log('\n' + '='.repeat(80))
  console.log('✅ 测试完成！')
  console.log('\n建议:')
  console.log('  1. 查看完整日志: gcloud run services logs read browser-exec --region=asia-northeast1 --limit=200')
  console.log('  2. 监控队列: gcloud pubsub subscriptions describe browser-visit-workers')
  console.log('  3. 检查失败原因并针对性优化配置')
}

main().catch(console.error)
