/**
 * 测试 browser-exec 预发环境 - 通过 HTTP API 直接调用
 *
 * 测试4个广告联盟 Offer URL，检测是否能访问到最终落地页
 */

import fetch from 'node-fetch'

// 预发环境 browser-exec URL
const BROWSER_EXEC_URL = 'https://browser-exec-yt54xvsg5q-an.a.run.app'

// 测试的4个 Offer URL
const OFFER_URLS = [
  {
    name: "pboost.me",
    url: "https://pboost.me/ZDO2Bdek"
  },
  {
    name: "dognet.com (Dyson)",
    url: "https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F"
  },
  {
    name: "yeahpromos.com",
    url: "https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url="
  },
  {
    name: "bonusarrive.com",
    url: "https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink"
  }
]

// 美国代理配置
const PROXY_URL = process.env.PROXY_URL_US || "https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt"

async function testOfferURL(offer) {
  const startTime = Date.now()

  try {
    console.log(`\n🔍 测试: ${offer.name}`)
    console.log(`   URL: ${offer.url}`)

    const response = await fetch(`${BROWSER_EXEC_URL}/api/v1/browser/visit`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        url: offer.url,
        targetCountry: 'US',
        proxyProviderURL: PROXY_URL,
        proxyPoolSize: 10,
        refererStrategy: 'social',
        maxRetries: 1,
        advancedOptions: {
          timeout: 45000,
          stabilizeMs: 15000,
          enableAntiBot: true
        }
      })
    })

    const duration = Date.now() - startTime
    const result = await response.json()

    // 分析结果
    console.log(`   ⏱️  耗时: ${duration}ms`)
    console.log(`   📊 状态: ${response.status}`)

    if (result.success) {
      console.log(`   ✅ 成功访问`)
      console.log(`   🎯 最终域名: ${result.result.domain}`)
      console.log(`   🏷️  品牌名称: ${result.result.brandName || 'N/A'}`)
      console.log(`   🔗 最终 URL: ${result.result.finalUrl}`)
      console.log(`   📈 HTTP状态: ${result.result.statusCode}`)

      if (result.result.isIntermediatePage?.isIntermediate) {
        console.log(`   ⚠️  警告: 停留在中间页 (${result.result.failureReason})`)
        return {
          ...offer,
          success: false,
          status: 'intermediate_page',
          duration,
          details: result
        }
      }

      if (result.result.available === false) {
        console.log(`   ❌ Offer 不可用 (${result.result.failureReason})`)
        return {
          ...offer,
          success: false,
          status: 'unavailable',
          duration,
          details: result
        }
      }

      return {
        ...offer,
        success: true,
        status: 'success',
        domain: result.result.domain,
        brandName: result.result.brandName,
        finalUrl: result.result.finalUrl,
        duration,
        details: result
      }
    } else {
      console.log(`   ❌ 访问失败`)
      console.log(`   错误: ${result.error?.message || 'Unknown error'}`)

      return {
        ...offer,
        success: false,
        status: 'error',
        error: result.error?.message,
        duration,
        details: result
      }
    }

  } catch (error) {
    const duration = Date.now() - startTime
    console.log(`   ❌ 异常: ${error.message}`)

    return {
      ...offer,
      success: false,
      status: 'exception',
      error: error.message,
      duration
    }
  }
}

async function main() {
  console.log('🚀 browser-exec HTTP API 测试开始\n')
  console.log('=' .repeat(80))

  const results = []

  // 串行测试每个 URL
  for (const offer of OFFER_URLS) {
    const result = await testOfferURL(offer)
    results.push(result)

    // 等待1秒避免速率限制
    await new Promise(r => setTimeout(r, 1000))
  }

  // 汇总结果
  console.log('\n' + '='.repeat(80))
  console.log('\n📊 测试结果汇总:\n')

  const successful = results.filter(r => r.success)
  const failed = results.filter(r => !r.success)

  console.log(`总计: ${results.length} 个 URL`)
  console.log(`✅ 成功: ${successful.length} (${(successful.length/results.length*100).toFixed(1)}%)`)
  console.log(`❌ 失败: ${failed.length} (${(failed.length/results.length*100).toFixed(1)}%)`)

  if (successful.length > 0) {
    console.log('\n成功访问的 Offer:')
    successful.forEach(r => {
      console.log(`  • ${r.name}: ${r.domain} (${r.brandName}) - ${r.duration}ms`)
    })
  }

  if (failed.length > 0) {
    console.log('\n失败的 Offer:')
    failed.forEach(r => {
      console.log(`  • ${r.name}: ${r.status} - ${r.error || r.details?.result?.failureReason || 'Unknown'}`)
    })
  }

  // 性能统计
  const avgDuration = results.reduce((sum, r) => sum + (r.duration || 0), 0) / results.length
  console.log(`\n⏱️  平均耗时: ${avgDuration.toFixed(0)}ms`)

  console.log('\n' + '='.repeat(80))
  console.log('✅ 测试完成！\n')
}

main().catch(console.error)
