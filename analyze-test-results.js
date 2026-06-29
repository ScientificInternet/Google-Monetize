#!/usr/bin/env node
/**
 * 分析 400 URL 测试的最终结果
 * 从 Cloud Logging 提取详细数据并生成报告
 */

const { exec } = require('child_process')
const util = require('util')
const fs = require('fs').promises
const execPromise = util.promisify(exec)

const PROJECT_ID = '${GCP_PROJECT_ID}'
const SERVICE_NAME = 'browser-exec-preview-worker'

async function fetchAllResults(startTime) {
  console.log('🔍 从 Cloud Logging 获取结果...')
  
  // 获取所有处理完成的日志
  const cmd = `gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=${SERVICE_NAME} AND timestamp>='${startTime}' AND textPayload=~'processed successfully'" --limit 1000 --format json --project ${PROJECT_ID}`
  
  const { stdout } = await execPromise(cmd)
  const logs = JSON.parse(stdout)
  
  console.log(`✅ 获取到 ${logs.length} 条日志`)
  
  // 解析结果
  const results = {
    total: 0,
    success: 0,
    failed: 0,
    byUrl: {},
    durations: [],
    errors: {}
  }
  
  logs.forEach(log => {
    const text = log.textPayload || ''
    
    // 提取 URL
    let url = 'unknown'
    if (text.includes('pboost')) url = 'pboost.me'
    else if (text.includes('dognet')) url = 'dognet.com'
    else if (text.includes('yeahpromos')) url = 'yeahpromos.com'
    else if (text.includes('bonusarrive')) url = 'bonusarrive.com'
    
    // 提取结果
    const successMatch = text.match(/URL访问(成功|失败)/)
    if (!successMatch) return
    
    const isSuccess = successMatch[1] === '成功'
    
    // 提取耗时
    const durationMatch = text.match(/\((\d+)ms\)/)
    const duration = durationMatch ? parseInt(durationMatch[1]) : 0
    
    // 提取失败原因
    let failureReason = null
    if (!isSuccess) {
      const reasonMatch = text.match(/失败原因: (.+)$/)
      failureReason = reasonMatch ? reasonMatch[1] : 'Unknown'
    }
    
    // 统计
    results.total++
    if (isSuccess) {
      results.success++
    } else {
      results.failed++
      if (failureReason) {
        results.errors[failureReason] = (results.errors[failureReason] || 0) + 1
      }
    }
    
    if (duration > 0) {
      results.durations.push(duration)
    }
    
    // 按 URL 统计
    if (!results.byUrl[url]) {
      results.byUrl[url] = {
        total: 0,
        success: 0,
        failed: 0,
        durations: []
      }
    }
    
    results.byUrl[url].total++
    if (isSuccess) {
      results.byUrl[url].success++
    } else {
      results.byUrl[url].failed++
    }
    if (duration > 0) {
      results.byUrl[url].durations.push(duration)
    }
  })
  
  return results
}

function generateReport(results) {
  let report = []
  
  report.push('# 400 URL 批量测试结果报告')
  report.push('')
  report.push(`**测试时间**: ${new Date().toLocaleString('zh-CN')}`)
  report.push(`**测试数量**: 400 (4 URL x 100 次)`)
  report.push('')
  report.push('---')
  report.push('')
  
  // 总体统计
  const successRate = ((results.success / results.total) * 100).toFixed(1)
  const avgDuration = results.durations.length > 0 
    ? (results.durations.reduce((a, b) => a + b, 0) / results.durations.length / 1000).toFixed(1)
    : 0
  
  report.push('## 总体统计')
  report.push('')
  report.push('| 指标 | 数值 |')
  report.push('|------|------|')
  report.push(`| **处理总数** | ${results.total}/400 |`)
  report.push(`| **成功数** | ${results.success} (${successRate}%) |`)
  report.push(`| **失败数** | ${results.failed} (${(100 - successRate).toFixed(1)}%) |`)
  report.push(`| **平均耗时** | ${avgDuration}s |`)
  
  if (results.durations.length > 0) {
    const sorted = [...results.durations].sort((a, b) => a - b)
    const min = (sorted[0] / 1000).toFixed(1)
    const max = (sorted[sorted.length - 1] / 1000).toFixed(1)
    const median = (sorted[Math.floor(sorted.length / 2)] / 1000).toFixed(1)
    
    report.push(`| **最快** | ${min}s |`)
    report.push(`| **最慢** | ${max}s |`)
    report.push(`| **中位数** | ${median}s |`)
  }
  
  report.push('')
  report.push('---')
  report.push('')
  
  // 按 URL 统计
  report.push('## 按 URL 统计')
  report.push('')
  report.push('| URL | 处理数 | 成功 | 失败 | 成功率 | 平均耗时 |')
  report.push('|-----|--------|------|------|--------|----------|')
  
  Object.entries(results.byUrl).forEach(([url, data]) => {
    const rate = ((data.success / data.total) * 100).toFixed(1)
    const avg = data.durations.length > 0
      ? (data.durations.reduce((a, b) => a + b, 0) / data.durations.length / 1000).toFixed(1)
      : 0
    report.push(`| ${url} | ${data.total}/100 | ${data.success} | ${data.failed} | ${rate}% | ${avg}s |`)
  })
  
  report.push('')
  report.push('---')
  report.push('')
  
  // 失败原因分析
  if (Object.keys(results.errors).length > 0) {
    report.push('## 失败原因分析')
    report.push('')
    report.push('| 失败原因 | 次数 | 占比 |')
    report.push('|----------|------|------|')
    
    const sortedErrors = Object.entries(results.errors)
      .sort((a, b) => b[1] - a[1])
    
    sortedErrors.forEach(([reason, count]) => {
      const percent = ((count / results.failed) * 100).toFixed(1)
      report.push(`| ${reason} | ${count} | ${percent}% |`)
    })
    
    report.push('')
  }
  
  return report.join('\n')
}

async function main() {
  console.log('='.repeat(70))
  console.log('📊 分析 400 URL 测试结果')
  console.log('='.repeat(70))
  console.log()
  
  // 获取开始时间（从现在往前推 2 小时）
  const startTime = new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString()
  
  const results = await fetchAllResults(startTime)
  
  console.log('\n分析完成！')
  console.log(`处理: ${results.total}/400`)
  console.log(`成功: ${results.success} (${((results.success / results.total) * 100).toFixed(1)}%)`)
  console.log(`失败: ${results.failed} (${((results.failed / results.total) * 100).toFixed(1)}%)`)
  
  // 生成报告
  console.log('\n生成报告...')
  const report = generateReport(results)
  
  const reportPath = `test-400-urls-report-${Date.now()}.md`
  await fs.writeFile(reportPath, report)
  
  console.log(`\n✅ 报告已保存: ${reportPath}`)
  console.log()
  console.log('预览:')
  console.log('-'.repeat(70))
  console.log(report.split('\n').slice(0, 30).join('\n'))
  console.log('...')
  console.log('='.repeat(70))
}

main().catch(err => {
  console.error('错误:', err.message)
  process.exit(1)
})
