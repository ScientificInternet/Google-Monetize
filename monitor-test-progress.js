#!/usr/bin/env node
/**
 * 监控 400 个 URL 测试的处理进度
 * 实时从 Cloud Logging 获取处理结果
 */

const { exec } = require('child_process')
const util = require('util')
const execPromise = util.promisify(exec)

const PROJECT_ID = '${GCP_PROJECT_ID}'
const SERVICE_NAME = 'browser-exec-preview-worker'

// 统计数据
const stats = {
  total: 400,
  processed: 0,
  success: 0,
  failed: 0,
  byUrl: {}
}

// 从日志中提取结果
async function fetchResults(since = '10m') {
  const cmd = `gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=${SERVICE_NAME} AND textPayload=~'processed successfully'" --limit 500 --format "value(timestamp,textPayload)" --project ${PROJECT_ID} --freshness=${since}`
  
  try {
    const { stdout } = await execPromise(cmd)
    const lines = stdout.trim().split('\n').filter(l => l)
    
    lines.forEach(line => {
      // 解析日志: "2025-10-02T13:19:55.862671Z [pubsub] Message 16565955792970123 processed successfully - URL访问成功 (167444ms)"
      const match = line.match(/processed successfully - URL访问(成功|失败)/)
      if (match) {
        stats.processed++
        if (match[1] === '成功') {
          stats.success++
        } else {
          stats.failed++
        }
        
        // 尝试提取 URL
        const urlMatch = line.match(/(pboost|dognet|yeahpromos|bonusarrive)/)
        if (urlMatch) {
          const url = urlMatch[1]
          if (!stats.byUrl[url]) {
            stats.byUrl[url] = { success: 0, failed: 0, total: 0 }
          }
          stats.byUrl[url].total++
          if (match[1] === '成功') {
            stats.byUrl[url].success++
          } else {
            stats.byUrl[url].failed++
          }
        }
      }
    })
  } catch (err) {
    // 忽略错误，可能是没有日志
  }
}

// 显示进度
function displayProgress() {
  console.clear()
  console.log('='.repeat(70))
  console.log('📊 400 URL 批量测试进度监控')
  console.log('='.repeat(70))
  console.log(`更新时间: ${new Date().toLocaleTimeString('zh-CN')}`)
  console.log()
  
  // 总体进度
  const progress = ((stats.processed / stats.total) * 100).toFixed(1)
  const successRate = stats.processed > 0 ? ((stats.success / stats.processed) * 100).toFixed(1) : 0
  
  console.log('📈 总体进度:')
  console.log(`   处理进度: ${stats.processed}/${stats.total} (${progress}%)`)
  console.log(`   成功: ${stats.success} (${successRate}%)`)
  console.log(`   失败: ${stats.failed} (${(100 - successRate).toFixed(1)}%)`)
  
  // 进度条
  const barWidth = 50
  const filled = Math.floor((stats.processed / stats.total) * barWidth)
  const bar = '█'.repeat(filled) + '░'.repeat(barWidth - filled)
  console.log(`   [${bar}] ${progress}%`)
  
  console.log()
  console.log('📊 按 URL 统计:')
  console.log('-'.repeat(70))
  
  const urls = {
    'pboost': 'pboost.me',
    'dognet': 'dognet.com', 
    'yeahpromos': 'yeahpromos.com',
    'bonusarrive': 'bonusarrive.com'
  }
  
  Object.entries(urls).forEach(([key, name]) => {
    const data = stats.byUrl[key] || { total: 0, success: 0, failed: 0 }
    const rate = data.total > 0 ? ((data.success / data.total) * 100).toFixed(1) : 0
    console.log(`   ${name.padEnd(20)} ${data.total}/100  成功: ${data.success}  失败: ${data.failed}  (${rate}%)`)
  })
  
  console.log()
  console.log('💡 提示: 按 Ctrl+C 退出监控')
  console.log('='.repeat(70))
}

// 主循环
async function main() {
  console.log('启动监控...\n')
  
  // 每 10 秒更新一次
  setInterval(async () => {
    await fetchResults('15m')
    displayProgress()
  }, 10000)
  
  // 立即执行一次
  await fetchResults('15m')
  displayProgress()
}

main().catch(err => {
  console.error('错误:', err.message)
  process.exit(1)
})
