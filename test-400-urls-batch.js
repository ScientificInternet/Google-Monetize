#!/usr/bin/env node
/**
 * 批量测试 400 个 URL（4 个 URL x 100 次）
 * 通过 Pub/Sub 队列异步处理，最大化并发，减少耗时
 */

const BASE_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app'
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt'

const TEST_URLS = [
  'https://pboost.me/ZDO2Bdek',
  'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
  'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
  'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink'
]

const TIMES_PER_URL = 100
const TOTAL_TASKS = TEST_URLS.length * TIMES_PER_URL

console.log('='.repeat(70))
console.log('批量测试 400 个广告联盟 URL (Pub/Sub 队列模式)')
console.log('='.repeat(70))
console.log(`测试服务: ${BASE_URL}`)
console.log(`代理服务: ${PROXY_URL.substring(0, 60)}...`)
console.log(`测试 URL 数量: ${TEST_URLS.length}`)
console.log(`每个 URL 测试次数: ${TIMES_PER_URL}`)
console.log(`总任务数: ${TOTAL_TASKS}`)
console.log('='.repeat(70))
console.log()

// 生成所有任务
function generateTasks() {
  const tasks = []
  for (const url of TEST_URLS) {
    for (let i = 0; i < TIMES_PER_URL; i++) {
      tasks.push({
        url,
        proxyUrl: PROXY_URL,
        index: i + 1,
        shortUrl: url.split('?')[0].replace(/https?:\/\/(www\.)?/, '')
      })
    }
  }
  return tasks
}

// 批量发布任务到 Pub/Sub 队列
async function publishBatch(tasks, batchSize = 50) {
  console.log(`\n📤 发布任务到 Pub/Sub 队列 (批次大小: ${batchSize})`)
  console.log('-'.repeat(70))
  
  const startTime = Date.now()
  let published = 0
  let failed = 0
  
  // 分批发布
  for (let i = 0; i < tasks.length; i += batchSize) {
    const batch = tasks.slice(i, i + batchSize)
    const batchNum = Math.floor(i / batchSize) + 1
    const totalBatches = Math.ceil(tasks.length / batchSize)
    
    process.stdout.write(`\r批次 ${batchNum}/${totalBatches}: 发布中...`)
    
    // 并发发布当前批次
    const promises = batch.map(task => 
      fetch(`${BASE_URL}/api/v1/browser/visit-queue`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: task.url,
          proxyProviderURL: task.proxyUrl  // 修复：使用正确的参数名 proxyProviderURL
        })
      })
      .then(res => res.json())
      .then(data => {
        if (data.status === 'queued') {
          published++
          return { success: true, messageId: data.messageId }
        } else {
          failed++
          return { success: false, error: data.error }
        }
      })
      .catch(err => {
        failed++
        return { success: false, error: err.message }
      })
    )
    
    await Promise.all(promises)
    
    process.stdout.write(`\r批次 ${batchNum}/${totalBatches}: 完成 (${published}/${tasks.length})`)
  }
  
  const duration = ((Date.now() - startTime) / 1000).toFixed(2)
  
  console.log('\n' + '-'.repeat(70))
  console.log(`✅ 发布完成`)
  console.log(`   总耗时: ${duration}s`)
  console.log(`   成功: ${published}/${tasks.length}`)
  console.log(`   失败: ${failed}/${tasks.length}`)
  console.log(`   发布速率: ${(tasks.length / duration).toFixed(1)} 任务/秒`)
  
  return { published, failed, duration }
}

// 主函数
async function main() {
  const allStartTime = Date.now()
  
  // 生成任务
  console.log('\n📋 生成任务列表...')
  const tasks = generateTasks()
  console.log(`✅ 生成了 ${tasks.length} 个任务`)
  
  // 按 URL 分组统计
  const urlGroups = {}
  tasks.forEach(task => {
    const key = task.shortUrl
    if (!urlGroups[key]) urlGroups[key] = 0
    urlGroups[key]++
  })
  
  console.log('\n任务分布:')
  Object.entries(urlGroups).forEach(([url, count]) => {
    console.log(`  - ${url}: ${count} 次`)
  })
  
  // 发布任务
  const publishResult = await publishBatch(tasks, 50)
  
  if (publishResult.failed > 0) {
    console.log(`\n⚠️  有 ${publishResult.failed} 个任务发布失败`)
  }
  
  // 总结
  const totalDuration = ((Date.now() - allStartTime) / 1000).toFixed(2)
  console.log('\n' + '='.repeat(70))
  console.log('📊 测试启动完成')
  console.log('='.repeat(70))
  console.log(`总耗时: ${totalDuration}s`)
  console.log(`已发布: ${publishResult.published} 个任务到队列`)
  console.log()
  console.log('💡 后续步骤:')
  console.log('   1. Worker 实例会自动消费队列（5-20 个实例并发处理）')
  console.log('   2. 使用以下命令监控处理进度:')
  console.log(`      gcloud logging read "resource.labels.service_name=browser-exec-preview-worker" --limit 100 --project ${GCP_PROJECT_ID}`)
  console.log('   3. 预计完成时间:')
  
  // 估算完成时间
  const avgTimePerUrl = 480 // 8 分钟（基于之前的测试）
  const concurrency = 50 // 保守估计 50 并发（5 实例 x 10 并发）
  const estimatedSeconds = Math.ceil((tasks.length * avgTimePerUrl) / concurrency)
  const estimatedMinutes = Math.ceil(estimatedSeconds / 60)
  
  console.log(`      并发处理: ${concurrency} 任务`)
  console.log(`      预计耗时: ${estimatedMinutes} 分钟`)
  console.log(`      预计完成时间: ${new Date(Date.now() + estimatedSeconds * 1000).toLocaleTimeString('zh-CN')}`)
  console.log()
  console.log('🔍 查看实时结果统计:')
  console.log(`   node monitor-test-progress.js`)
  console.log('='.repeat(70))
}

main().catch(err => {
  console.error('\n❌ 错误:', err.message)
  process.exit(1)
})
