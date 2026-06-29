#!/usr/bin/env node

/**
 * Test script to verify CSRF token is properly set on login page
 */

async function testCsrfToken() {
  const url = 'https://www.urlchecker.dev/auth/sign-in';

  console.log('Testing CSRF token on login page...');
  console.log('URL:', url);
  console.log('');

  try {
    const response = await fetch(url, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
      }
    });

    console.log('Response status:', response.status);

    // Check cookies
    const cookies = response.headers.get('set-cookie');
    console.log('\nCookies set by server:');
    if (cookies) {
      const cookieLines = cookies.split(',').map(c => c.trim());
      cookieLines.forEach(cookie => {
        if (cookie.includes('csrf')) {
          console.log('  ✅', cookie.substring(0, 100));
        } else {
          console.log('  -', cookie.substring(0, 80));
        }
      });

      const hasCsrfSecret = cookies.includes('csrfSecret=');
      console.log('\n' + (hasCsrfSecret ? '✅' : '❌') + ' csrfSecret cookie found');
    } else {
      console.log('  ❌ No cookies found');
    }

    // Check HTML for CSRF token in __NEXT_DATA__
    const html = await response.text();

    const nextDataMatch = html.match(/<script id="__NEXT_DATA__"[^>]*>(.*?)<\/script>/s);
    if (nextDataMatch) {
      try {
        const nextData = JSON.parse(nextDataMatch[1]);
        const csrfToken = nextData?.props?.pageProps?.csrfToken;

        console.log('\n__NEXT_DATA__ analysis:');
        console.log('  csrfToken in pageProps:', csrfToken ? '✅ ' + csrfToken.substring(0, 20) + '...' : '❌ NOT FOUND');

        if (csrfToken) {
          console.log('\n✅ CSRF token is properly injected into page props');
        } else {
          console.log('\n❌ CSRF token is missing from page props');
          console.log('   This will cause 401 errors when creating session');
        }
      } catch (e) {
        console.log('\n❌ Failed to parse __NEXT_DATA__:', e.message);
      }
    } else {
      console.log('\n❌ __NEXT_DATA__ script not found in HTML');
    }

  } catch (error) {
    console.error('\n❌ Error:', error.message);
  }
}

testCsrfToken();
