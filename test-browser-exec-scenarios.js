/**
 * Browser-Exec 服务场景测试脚本
 * 测试 Offer URL 访问的 4 个场景
 */

const OFFER_URL = 'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F'
const PROXY_URL_US = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt'
// 使用预发环境的 browser-exec 服务
const BROWSER_EXEC_URL = process.env.BROWSER_EXEC_URL || 'https://browser-exec-preview-644672509127.asia-northeast1.run.app/api/v1/browser'

// 测试结果存储
const results = {
  scenario1: null,
  scenario2: [],
  scenario3: null,
  scenario4: null
}

/**
 * 场景1: Offer评估 - 获取落地页域名
 */
async function testScenario1() {
  console.log('\n=== 场景1: Offer评估 - 获取落地页域名 ===')
  const startTime = Date.now()

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: OFFER_URL,
        proxyProviderURL: PROXY_URL_US,
        refererStrategy: 'social',
        targetCountry: 'US'
      })
    })

    const data = await response.json()
    const duration = Date.now() - startTime

    results.scenario1 = {
      success: data.success,
      statusCode: response.status,
      duration,
      domain: data.result?.domain,
      brandName: data.result?.brandName,
      finalUrl: data.result?.finalUrl,
      available: data.result?.available,
      proxyUsed: data.metadata?.proxyUsed,
      error: data.error
    }

    console.log('✅ 测试完成')
    console.log(`   耗时: ${duration}ms`)
    console.log(`   成功: ${data.success}`)
    console.log(`   域名: ${data.result?.domain || 'N/A'}`)
    console.log(`   品牌: ${data.result?.brandName || 'N/A'}`)
    console.log(`   代理: ${data.metadata?.proxyUsed ? '是' : '否'}`)

  } catch (error) {
    console.error('❌ 测试失败:', error.message)
    results.scenario1 = { success: false, error: error.message }
  }
}

/**
 * 场景2: 补点击任务 - 10次访问使用不同代理IP
 * 使用串行执行避免并发过载
 */
async function testScenario2() {
  console.log('\n=== 场景2: 补点击任务 - 10次访问使用不同代理IP ===')
  console.log('   使用串行执行，避免服务过载...\n')

  const testResults = []

  for (let i = 1; i <= 10; i++) {
    const startTime = Date.now()
    console.log(`   [${i}/10] 开始访问...`)

    try {
      const response = await fetch(`${BROWSER_EXEC_URL}/visit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: OFFER_URL,
          proxyProviderURL: PROXY_URL_US,
          proxyPoolSize: 10, // 请求10个代理池
          refererStrategy: 'social',
          targetCountry: 'US'
        })
      })

      const data = await response.json()
      const duration = Date.now() - startTime

      const result = {
        index: i,
        success: data.success,
        duration,
        proxyServer: data.metadata?.proxyServer?.split(':')[0], // 只显示IP
        domain: data.result?.domain,
        available: data.result?.available,
        antiDetectionPassed: data.result?.antiDetectionResult?.passed,
        error: data.error
      }

      testResults.push(result)

      console.log(`   [${i}/10] ${data.success ? '✅' : '❌'} 耗时:${duration}ms 代理:${result.proxyServer || 'N/A'}`)

    } catch (error) {
      console.error(`   [${i}/10] ❌ 失败:`, error.message)
      testResults.push({ index: i, success: false, error: error.message })
    }

    // 短暂延迟，避免请求过快
    if (i < 10) {
      await new Promise(r => setTimeout(r, 500))
    }
  }

  results.scenario2 = testResults

  // 统计结果
  const successCount = results.scenario2.filter(r => r.success).length
  const uniqueProxies = new Set(results.scenario2.map(r => r.proxyServer).filter(Boolean)).size
  console.log(`\n   总计: ${successCount}/10 成功, ${uniqueProxies} 个不同代理IP`)
}

/**
 * 场景3: 换链接 - 获取 Final URL 和 Final URL suffix
 */
async function testScenario3() {
  console.log('\n=== 场景3: 换链接 - 获取 Final URL 和 Final URL suffix ===')
  const startTime = Date.now()

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: OFFER_URL,
        proxyProviderURL: PROXY_URL_US,
        refererStrategy: 'social',
        targetCountry: 'US'
      })
    })

    const data = await response.json()
    const duration = Date.now() - startTime

    results.scenario3 = {
      success: data.success,
      duration,
      finalUrl: data.result?.finalUrl,
      finalUrlSuffix: data.result?.finalUrlSuffix,
      redirectChain: data.result?.redirectChain,
      redirectCount: data.timings?.redirectCount,
      error: data.error
    }

    console.log('✅ 测试完成')
    console.log(`   耗时: ${duration}ms`)
    console.log(`   Final URL: ${data.result?.finalUrl || 'N/A'}`)
    console.log(`   Final URL Suffix: ${data.result?.finalUrlSuffix || 'N/A'}`)
    console.log(`   重定向次数: ${data.timings?.redirectCount || 0}`)

  } catch (error) {
    console.error('❌ 测试失败:', error.message)
    results.scenario3 = { success: false, error: error.message }
  }
}

/**
 * 场景4: 风险识别 - 落地页可用性检测
 */
async function testScenario4() {
  console.log('\n=== 场景4: 风险识别 - 落地页可用性检测 ===')
  const startTime = Date.now()

  try {
    const response = await fetch(`${BROWSER_EXEC_URL}/visit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: OFFER_URL,
        proxyProviderURL: PROXY_URL_US,
        refererStrategy: 'social',
        targetCountry: 'US'
      })
    })

    const data = await response.json()
    const duration = Date.now() - startTime

    results.scenario4 = {
      success: data.success,
      duration,
      available: data.result?.available,
      statusCode: data.result?.statusCode,
      domain: data.result?.domain,
      isIntermediatePage: data.result?.isIntermediatePage,
      failureReason: data.result?.failureReason,
      error: data.error
    }

    console.log('✅ 测试完成')
    console.log(`   耗时: ${duration}ms`)
    console.log(`   可用性: ${data.result?.available ? '✅ 可用' : '❌ 不可用'}`)
    console.log(`   HTTP状态: ${data.result?.statusCode || 'N/A'}`)
    console.log(`   是否中间页: ${data.result?.isIntermediatePage ? '是' : '否'}`)
    if (data.result?.failureReason) {
      console.log(`   失败原因: ${data.result.failureReason}`)
    }

  } catch (error) {
    console.error('❌ 测试失败:', error.message)
    results.scenario4 = { success: false, error: error.message }
  }
}

/**
 * 分析测试结果
 */
function analyzeResults() {
  console.log('\n\n' + '='.repeat(80))
  console.log('测试结果分析')
  console.log('='.repeat(80))

  // 场景1分析
  console.log('\n【场景1: Offer评估】')
  if (results.scenario1?.success) {
    console.log(`✅ 成功获取落地页域名: ${results.scenario1.domain}`)
    console.log(`   品牌: ${results.scenario1.brandName}`)
    console.log(`   耗时: ${results.scenario1.duration}ms`)
  } else {
    console.log(`❌ 失败: ${results.scenario1?.error || 'Unknown'}`)
  }

  // 场景2分析
  console.log('\n【场景2: 补点击任务】')
  if (results.scenario2.length > 0) {
    const successCount = results.scenario2.filter(r => r.success).length
    const successRate = (successCount / results.scenario2.length * 100).toFixed(1)
    const uniqueProxies = new Set(results.scenario2.map(r => r.proxyServer).filter(Boolean)).size
    const avgDuration = Math.round(
      results.scenario2.filter(r => r.duration).reduce((sum, r) => sum + r.duration, 0) /
      results.scenario2.filter(r => r.duration).length
    )

    console.log(`✅ 成功率: ${successRate}% (${successCount}/${results.scenario2.length})`)
    console.log(`   不同代理IP数: ${uniqueProxies}`)
    console.log(`   平均耗时: ${avgDuration}ms`)
    console.log(`   突破风控: ${results.scenario2.filter(r => r.antiDetectionPassed).length}次`)
  } else {
    console.log('❌ 未执行测试')
  }

  // 场景3分析
  console.log('\n【场景3: 换链接】')
  if (results.scenario3?.success) {
    console.log(`✅ 成功获取 Final URL`)
    console.log(`   Final URL: ${results.scenario3.finalUrl}`)
    console.log(`   Suffix: ${results.scenario3.finalUrlSuffix || '(空)'}`)
    console.log(`   重定向次数: ${results.scenario3.redirectCount || 0}`)
    console.log(`   耗时: ${results.scenario3.duration}ms`)
  } else {
    console.log(`❌ 失败: ${results.scenario3?.error || 'Unknown'}`)
  }

  // 场景4分析
  console.log('\n【场景4: 落地页可用性检测】')
  if (results.scenario4?.success !== undefined) {
    if (results.scenario4.available) {
      console.log(`✅ 落地页可用`)
      console.log(`   域名: ${results.scenario4.domain}`)
      console.log(`   HTTP状态: ${results.scenario4.statusCode}`)
    } else {
      console.log(`❌ 落地页不可用`)
      console.log(`   原因: ${results.scenario4.failureReason || 'Unknown'}`)
      console.log(`   是否中间页: ${results.scenario4.isIntermediatePage ? '是' : '否'}`)
    }
    console.log(`   耗时: ${results.scenario4.duration}ms`)
  } else {
    console.log('❌ 未执行测试')
  }

  // 流量消耗估算
  console.log('\n【流量消耗估算】')
  const totalRequests = 1 + results.scenario2.length + 1 + 1 // 4个场景的总请求数
  const successRequests = [
    results.scenario1?.success ? 1 : 0,
    results.scenario2.filter(r => r.success).length,
    results.scenario3?.success ? 1 : 0,
    results.scenario4?.success ? 1 : 0
  ].reduce((a, b) => a + b, 0)

  // 基于资源阻断，每次访问约300KB
  const estimatedTrafficPerRequest = 300 // KB
  const totalTrafficKB = successRequests * estimatedTrafficPerRequest

  console.log(`   总请求数: ${totalRequests}`)
  console.log(`   成功请求数: ${successRequests}`)
  console.log(`   估算流量: ${totalTrafficKB} KB (${(totalTrafficKB / 1024).toFixed(2)} MB)`)
  console.log(`   平均每次: ${estimatedTrafficPerRequest} KB`)

  console.log('\n' + '='.repeat(80))
}

/**
 * 主函数
 */
async function main() {
  console.log('Browser-Exec 服务场景测试')
  console.log('测试 URL:', OFFER_URL)
  console.log('Browser-Exec 服务:', BROWSER_EXEC_URL)
  console.log('代理服务:', PROXY_URL_US.split('password=')[0] + 'password=***')

  try {
    // 依次执行4个场景测试
    await testScenario1()
    await testScenario2()
    await testScenario3()
    await testScenario4()

    // 分析结果
    analyzeResults()

    // 保存结果到文件
    const fs = await import('fs')
    fs.writeFileSync(
      '/Users/jason/Documents/Kiro/gm/docs/MarkerkitGo/BrowserExec_Test_Results.json',
      JSON.stringify(results, null, 2),
      'utf-8'
    )
    console.log('\n✅ 测试结果已保存到: docs/MarkerkitGo/BrowserExec_Test_Results.json')

  } catch (error) {
    console.error('测试执行失败:', error)
    process.exit(1)
  }
}

// 运行测试
main()
