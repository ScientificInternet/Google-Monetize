/**
 * 测试AI评估 - 使用真实SimilarWeb数据
 *
 * 使用Playwright获取nike.com的真实SimilarWeb数据，然后测试AI评估prompt
 */

import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

async function fetchSimilarWebData(domain) {
  console.log(`正在获取 ${domain} 的SimilarWeb数据...`);

  const browser = await chromium.launch({
    headless: false, // 使用有头模式以便观察
    channel: 'chrome' // 使用本地已安装的Chrome
  });

  const context = await browser.newContext({
    userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
  });

  const page = await context.newPage();

  try {
    const url = `https://data.similarweb.com/api/v1/data?domain=${domain}`;
    console.log(`访问URL: ${url}`);

    // 访问API endpoint
    const response = await page.goto(url, {
      waitUntil: 'networkidle',
      timeout: 30000
    });

    // 获取响应内容
    const content = await page.content();
    console.log('页面内容:', content.substring(0, 500));

    // 尝试提取JSON数据
    const preTag = await page.locator('pre').first();
    let jsonData = null;

    if (await preTag.count() > 0) {
      const textContent = await preTag.textContent();
      jsonData = JSON.parse(textContent);
    } else {
      // 尝试从body获取
      const bodyText = await page.locator('body').textContent();
      jsonData = JSON.parse(bodyText);
    }

    console.log('成功获取SimilarWeb数据!');
    console.log(JSON.stringify(jsonData, null, 2));

    await browser.close();
    return jsonData;

  } catch (error) {
    console.error('获取数据失败:', error);
    await browser.close();
    throw error;
  }
}

async function testAIEvaluation() {
  try {
    // 1. 获取真实SimilarWeb数据
    const domain = 'nike.com';
    const similarWebData = await fetchSimilarWebData(domain);

    // 2. 保存数据到文件
    const dataFile = path.join(__dirname, 'nike_similarweb_data.json');
    fs.writeFileSync(dataFile, JSON.stringify(similarWebData, null, 2));
    console.log(`\n数据已保存到: ${dataFile}`);

    // 3. 构建AI评估输入
    const evaluationInput = {
      domain: domain,
      brandName: 'Nike',
      landingPageURL: 'https://www.nike.com',
      similarWebData: similarWebData
    };

    // 4. 保存评估输入
    const inputFile = path.join(__dirname, 'ai_evaluation_input.json');
    fs.writeFileSync(inputFile, JSON.stringify(evaluationInput, null, 2));
    console.log(`评估输入已保存到: ${inputFile}`);

    console.log('\n✅ 测试数据准备完成!');
    console.log('\n接下来需要:');
    console.log('1. 使用此数据调用Gemini API进行评估');
    console.log('2. 检查AI评估结果的质量');
    console.log('3. 根据结果优化prompt');

  } catch (error) {
    console.error('测试失败:', error);
    process.exit(1);
  }
}

// 运行测试
testAIEvaluation();
