#!/usr/bin/env node

/**
 * Test Pattern Library Implementation
 *
 * Tests the new pattern-based intermediate page detection system
 */

import { patternMatcher } from './services/browser-exec/pattern-matcher.js'

console.log('='.repeat(80))
console.log('Pattern Library Test Suite')
console.log('='.repeat(80))
console.log('')

// Test 1: Load pattern library
console.log('【Test 1】Pattern Library Loading')
console.log('-'.repeat(80))
const stats = patternMatcher.getStats()
console.log('Pattern Library Stats:')
console.log(`  Version: ${stats.version}`)
console.log(`  Last Updated: ${stats.lastUpdated}`)
console.log(`  Domain Patterns: ${stats.domainPatterns}`)
console.log(`  Text Patterns: ${stats.textPatterns}`)
console.log(`  DOM Patterns: ${stats.domPatterns}`)
console.log(`  Expired Indicators: ${stats.expiredIndicators}`)
console.log('')

// Test 2: Domain pattern matching
console.log('【Test 2】Domain Pattern Matching')
console.log('-'.repeat(80))

const testCases = [
  {
    name: 'chromewebdata verification page',
    features: {
      domain: 'chromewebdata.com',
      url: 'https://chromewebdata.com/verification',
      title: 'Verification',
      content: '<html><body>Checking your browser...</body></html>',
      urlPath: '/verification'
    },
    expected: { isIntermediate: true, reason: 'domain-pattern', subtype: 'challenge' }
  },
  {
    name: 'linkbux countdown page',
    features: {
      domain: 'linkbux.com',
      url: 'https://www.linkbux.com/redirect',
      title: 'Redirecting',
      content: '<html><body><div id="countdown">5</div></body></html>',
      urlPath: '/redirect'
    },
    expected: { isIntermediate: true, reason: 'domain-pattern', subtype: 'countdown' }
  },
  {
    name: 'dailybacks return.html countdown',
    features: {
      domain: 'dailybacks.com',
      url: 'https://dailybacks.com/return.html?id=error_suspended.html',
      title: 'Please wait',
      content: '<html><body>Redirecting in 5 seconds...</body></html>',
      urlPath: '/return.html'
    },
    expected: { isIntermediate: true, reason: 'domain-pattern', subtype: 'countdown' }
  },
  {
    name: 'pboost.me fast redirect',
    features: {
      domain: 'pboost.me',
      url: 'https://pboost.me/ZDO2Bdek',
      title: '',
      content: '<html><body></body></html>',
      urlPath: '/ZDO2Bdek'
    },
    expected: { isIntermediate: true, reason: 'domain-pattern', subtype: 'fast-redirect' }
  },
  {
    name: 'error_suspended.html (expired page)',
    features: {
      domain: 'dailybacks.com',
      url: 'https://dailybacks.com/error_suspended.html',
      title: 'Suspended',
      content: '<html><body>This offer has been suspended.</body></html>',
      urlPath: '/error_suspended.html'
    },
    expected: { isExpired: true, isFinalPage: true, isIntermediate: false }
  },
  {
    name: 'yitahome.com (landing page)',
    features: {
      domain: 'yitahome.com',
      url: 'https://www.yitahome.com/',
      title: 'YITAHOME｜Home Furniture & Decor',
      content: '<html><body><h1>Welcome to YITAHOME</h1><p>Shop our furniture...</p></body></html>',
      urlPath: '/'
    },
    expected: { isIntermediate: false, reason: 'no-match' }
  },
  {
    name: 'Text pattern: redirecting in title',
    features: {
      domain: 'unknown-domain.com',
      url: 'https://unknown-domain.com/page',
      title: 'Redirecting - Please Wait',
      content: '<html><body>Loading...</body></html>',
      urlPath: '/page'
    },
    expected: { isIntermediate: true, reason: 'text-pattern' }
  },
  {
    name: 'Text pattern: countdown extraction',
    features: {
      domain: 'unknown-domain.com',
      url: 'https://unknown-domain.com/page',
      title: 'Please wait',
      content: '<html><body>You will be redirected in 10 seconds...</body></html>',
      urlPath: '/page'
    },
    expected: { isIntermediate: true, reason: 'text-pattern', extractedCountdown: 10 }
  },
  {
    name: 'Cloudflare challenge page',
    features: {
      domain: 'fatcoupon.com',
      url: 'https://www.fatcoupon.com/',
      title: 'Just a moment...',
      content: '<html><body>Cloudflare - Checking if the site connection is secure</body></html>',
      urlPath: '/'
    },
    expected: { isIntermediate: true, reason: 'text-pattern' }
  },
  {
    name: 'Meta refresh (heuristic)',
    features: {
      domain: 'unknown-domain.com',
      url: 'https://unknown-domain.com/page',
      title: '',
      content: '<html><head><meta http-equiv="refresh" content="0;url=/landing"></head></html>',
      urlPath: '/page'
    },
    expected: { isIntermediate: true, reason: 'content-heuristic' }
  }
]

let passed = 0
let failed = 0

for (const testCase of testCases) {
  const result = await patternMatcher.detectIntermediatePage(testCase.features)

  let success = true
  let errors = []

  // Check expected properties
  for (const [key, expectedValue] of Object.entries(testCase.expected)) {
    if (result[key] !== expectedValue) {
      success = false
      errors.push(`  ❌ Expected ${key}: ${expectedValue}, got: ${result[key]}`)
    }
  }

  if (success) {
    passed++
    console.log(`✅ ${testCase.name}`)
    console.log(`   Result: ${JSON.stringify(result, null, 2).replace(/\n/g, '\n   ')}`)
  } else {
    failed++
    console.log(`❌ ${testCase.name}`)
    console.log(`   Expected: ${JSON.stringify(testCase.expected)}`)
    console.log(`   Got: ${JSON.stringify(result)}`)
    errors.forEach(e => console.log(e))
  }
  console.log('')
}

// Test 3: Pattern management API simulation
console.log('【Test 3】Pattern Management')
console.log('-'.repeat(80))

// Add a new domain pattern
console.log('Adding new domain pattern...')
try {
  patternMatcher.addDomainPattern({
    id: 'test-network',
    domain: 'test-network.com',
    type: 'intermediate',
    subtype: 'fast-redirect',
    confidence: 0.9,
    expectedWaitTime: 2000,
    notes: 'Test network pattern'
  })
  console.log('✅ Pattern added successfully')
} catch (e) {
  console.log(`❌ Failed to add pattern: ${e.message}`)
}

// Update the pattern
console.log('\nUpdating pattern...')
try {
  patternMatcher.updateDomainPattern('test-network', {
    confidence: 0.95,
    notes: 'Updated test network pattern'
  })
  console.log('✅ Pattern updated successfully')
} catch (e) {
  console.log(`❌ Failed to update pattern: ${e.message}`)
}

// Delete the pattern
console.log('\nDeleting pattern...')
try {
  patternMatcher.deleteDomainPattern('test-network')
  console.log('✅ Pattern deleted successfully')
} catch (e) {
  console.log(`❌ Failed to delete pattern: ${e.message}`)
}

console.log('')

// Summary
console.log('='.repeat(80))
console.log('Test Summary')
console.log('='.repeat(80))
console.log(`Total: ${testCases.length}`)
console.log(`Passed: ${passed} (${(passed / testCases.length * 100).toFixed(1)}%)`)
console.log(`Failed: ${failed} (${(failed / testCases.length * 100).toFixed(1)}%)`)
console.log('')

if (failed === 0) {
  console.log('✅ All tests passed!')
} else {
  console.log(`⚠️  ${failed} test(s) failed`)
  process.exit(1)
}
