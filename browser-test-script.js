/**
 * 浏览器测试脚本
 *
 * 使用方法:
 * 1. 打开 https://frontend-preview-yt54xvsg5q-an.a.run.app/auth/sign-in
 * 2. 打开开发者工具Console (Cmd+Option+I)
 * 3. 复制粘贴这个脚本到Console并回车
 * 4. 脚本会自动监控OAuth流程并输出详细诊断信息
 */

(function () {
  console.log("🔍 Google登录诊断脚本已启动");
  console.log("================================");

  // 1. 检查Firebase配置
  console.log("\n📋 1. 检查Firebase配置");
  console.log("--------------------------------");

  const firebaseConfig = {
    apiKey: window.location.hostname,
    authDomain:
      document.querySelector('meta[name="firebase-auth-domain"]')?.content ||
      "未找到",
    projectId:
      document.querySelector('meta[name="firebase-project-id"]')?.content ||
      "未找到",
  };

  console.log("当前域名:", window.location.hostname);
  console.log("当前URL:", window.location.href);
  console.log("Firebase配置:", firebaseConfig);

  // 2. 检查IndexedDB
  console.log("\n💾 2. 检查IndexedDB");
  console.log("--------------------------------");

  const checkIndexedDB = () => {
    const request = indexedDB.open("firebaseLocalStorageDb");

    request.onsuccess = (event) => {
      const db = event.target.result;
      console.log("✅ IndexedDB可访问");
      console.log("数据库名称:", db.name);
      console.log("版本:", db.version);
      console.log("对象存储:", Array.from(db.objectStoreNames));

      // 尝试读取数据
      if (db.objectStoreNames.contains("firebaseLocalStorage")) {
        const transaction = db.transaction(
          ["firebaseLocalStorage"],
          "readonly",
        );
        const store = transaction.objectStore("firebaseLocalStorage");
        const getAllRequest = store.getAll();

        getAllRequest.onsuccess = () => {
          console.log("存储的数据数量:", getAllRequest.result.length);
          if (getAllRequest.result.length > 0) {
            console.log("数据示例:", getAllRequest.result[0]);
          }
        };
      }
    };

    request.onerror = () => {
      console.error("❌ IndexedDB访问失败:", request.error);
    };
  };

  checkIndexedDB();

  // 3. 检查Cookies
  console.log("\n🍪 3. 检查Cookies");
  console.log("--------------------------------");

  const cookies = document.cookie.split(";").map((c) => c.trim());
  const firebaseCookies = cookies.filter(
    (c) =>
      c.includes("firebase") || c.includes("__session") || c.includes("auth"),
  );

  console.log("总Cookie数量:", cookies.length);
  console.log("Firebase相关Cookie:", firebaseCookies.length);
  if (firebaseCookies.length > 0) {
    firebaseCookies.forEach((c) => {
      const [name] = c.split("=");
      console.log("  -", name);
    });
  } else {
    console.log("⚠️  未找到Firebase相关Cookie");
  }

  // 4. 检查LocalStorage
  console.log("\n📦 4. 检查LocalStorage");
  console.log("--------------------------------");

  const localStorageKeys = Object.keys(localStorage);
  const firebaseKeys = localStorageKeys.filter(
    (k) => k.includes("firebase") || k.includes("auth"),
  );

  console.log("总LocalStorage键数量:", localStorageKeys.length);
  console.log("Firebase相关键:", firebaseKeys.length);
  if (firebaseKeys.length > 0) {
    firebaseKeys.forEach((k) => {
      console.log("  -", k);
    });
  }

  // 5. 监控网络请求
  console.log("\n🌐 5. 网络请求监控已启动");
  console.log("--------------------------------");
  console.log("将监控以下请求:");
  console.log("  - accounts.google.com (OAuth)");
  console.log("  - firebaseapp.com (Auth Handler)");
  console.log("  - identitytoolkit.googleapis.com (Firebase Auth API)");

  // 拦截fetch
  const originalFetch = window.fetch;
  window.fetch = function (...args) {
    const url = args[0];
    if (typeof url === "string") {
      if (
        url.includes("google") ||
        url.includes("firebase") ||
        url.includes("identitytoolkit")
      ) {
        console.log("🌐 Fetch请求:", url);
      }
    }
    return originalFetch.apply(this, args);
  };

  // 6. 监控Console日志
  console.log("\n📝 6. OAuth流程监控");
  console.log("--------------------------------");
  console.log('等待用户点击"Sign in with Google"...');
  console.log("");
  console.log("💡 提示:");
  console.log("  1. 点击Google登录按钮");
  console.log("  2. 完成Google授权");
  console.log("  3. 返回后观察Console日志");
  console.log("  4. 查找以下关键日志:");
  console.log("     - [Sign In] Signing in with redirect");
  console.log("     - [OAuth Redirect] Checking for redirect result...");
  console.log("     - [OAuth Redirect] Credential received 或 User found");
  console.log("");

  // 7. 检查URL参数（如果是从OAuth redirect回来）
  const urlParams = new URLSearchParams(window.location.search);
  if (urlParams.has("state") || urlParams.has("code")) {
    console.log("🔄 检测到OAuth Redirect参数");
    console.log("--------------------------------");
    console.log("URL参数:");
    for (const [key, value] of urlParams.entries()) {
      if (key === "code" || key === "state") {
        console.log(`  ${key}:`, value.substring(0, 20) + "...");
      } else {
        console.log(`  ${key}:`, value);
      }
    }
  }

  // 8. 设置定时检查
  let checkCount = 0;
  const checkInterval = setInterval(() => {
    checkCount++;

    // 检查是否有Firebase Auth实例
    if (window.firebase || window.__FIREBASE_DEFAULTS__) {
      console.log(`\n✅ [${checkCount}s] Firebase已初始化`);

      // 尝试获取当前用户
      if (window.firebase?.auth) {
        const currentUser = window.firebase.auth().currentUser;
        if (currentUser) {
          console.log("✅ 当前用户:", {
            uid: currentUser.uid,
            email: currentUser.email,
            displayName: currentUser.displayName,
          });
          clearInterval(checkInterval);
        }
      }
    }

    // 30秒后停止检查
    if (checkCount >= 30) {
      console.log("\n⏱️  30秒检查完成");
      clearInterval(checkInterval);
    }
  }, 1000);

  console.log("\n✅ 诊断脚本设置完成！");
  console.log("================================");
  console.log("");
  console.log('现在可以点击"Sign in with Google"按钮进行测试');
  console.log("");
})();
