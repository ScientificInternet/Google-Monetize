#!/usr/bin/env node
// 测试 4 个 URL 各 1 次，验证修复

const BASE_URL = 'https://browser-exec-preview-644672509127.asia-northeast1.run.app'
const PROXY_URL = process.env.PROXY_URL_US || 'https://api.iprocket.io/api?username=YOUR_USERNAME&password=YOUR_PASSWORD&cc=ROW&ips=1&type=-res-&proxyType=http&responseType=txt'

const TEST_URLS = [
  'https://pboost.me/ZDO2Bdek',
  'https://go.dognet.com/?chid=6ab3CPGU&url=https%3A%2F%2Fwww.dyson.hr%2F',
  'https://yeahpromos.com/index/index/openurl?track=659f2181de1cb30f&url=',
  'https://www.bonusarrive.com/link?c=2375&ad=313850&url=&src=starlink'
]

async function test() {
  console.log('测试 4 个 URL（验证修复）\n')
  
  for (const url of TEST_URLS) {
    const shortUrl = url.split('?')[0].replace(/https?:\/\/(www\.)?/, '')
    process.stdout.write(`发布: ${shortUrl}... `)
    
    try {
      const res = await fetch(`${BASE_URL}/api/v1/browser/visit-queue`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url,
          proxyProviderURL: PROXY_URL  // 使用正确的参数名
        })
      })
      
      const data = await res.json()
      
      if (data.status === 'queued') {
        console.log(`✅ ${data.messageId}`)
      } else {
        console.log(`❌ ${data.error || 'failed'}`)
      }
    } catch (err) {
      console.log(`❌ ${err.message}`)
    }
  }
  
  console.log('\n✅ 4 个任务已发布到队列')
  console.log('等待 30 秒后检查结果...\n')
  
  await new Promise(r => setTimeout(r, 30000))
  
  console.log('提示：使用以下命令查看处理结果：')
  console.log('gcloud logging read "resource.labels.service_name=browser-exec-preview-worker AND textPayload=~\'processed successfully\'" --limit 10 --project ${GCP_PROJECT_ID}')
}

test()
