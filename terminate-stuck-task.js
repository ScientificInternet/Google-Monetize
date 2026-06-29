#!/usr/bin/env node

/**
 * 强制终止卡住的任务
 */

const taskId = process.argv[2];

if (!taskId) {
  console.error('请提供任务ID');
  console.log('用法: node terminate-stuck-task.js <taskId>');
  process.exit(1);
}

console.log(`正在终止任务: ${taskId}`);

// 首先尝试正常终止
const terminateUrl = `http://localhost:3000/api/batchopen/silent-terminate`;

fetch(terminateUrl, {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({ taskId })
})
.then(response => response.json())
.then(data => {
  console.log('终止响应:', data);
  
  // 检查任务状态
  setTimeout(() => {
    const checkUrl = `http://localhost:3000/api/batchopen/silent-progress?taskId=${taskId}`;
    fetch(checkUrl)
      .then(response => response.json())
      .then(status => {
        console.log('任务状态:', status);
        
        // 如果任务仍在运行，尝试强制清理
        if (status.success && status.status === 'running') {
          console.log('任务仍在运行，尝试强制清理...');
          
          const forceCleanupUrl = `http://localhost:3000/api/batchopen/force-cleanup`;
          
          fetch(forceCleanupUrl, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json'
            },
            body: JSON.stringify({ taskId, force: true })
          })
          .then(response => response.json())
          .then(forceData => {
            console.log('强制清理响应:', forceData);
            
            // 再次检查状态
            setTimeout(() => {
              fetch(checkUrl)
                .then(response => response.json())
                .then(finalStatus => {
                  console.log('最终任务状态:', finalStatus);
                })
                .catch(error => {
                  console.error('最终状态检查失败:', error);
                });
            }, 1000);
          })
          .catch(error => {
            console.error('强制清理失败:', error);
          });
        }
      })
      .catch(error => {
        console.error('检查状态失败:', error);
      });
  }, 1000);
})
.catch(error => {
  console.error('终止失败:', error);
  
  // 如果正常终止失败，直接尝试强制清理
  console.log('尝试强制清理...');
  
  const forceCleanupUrl = `http://localhost:3000/api/batchopen/force-cleanup`;
  
  fetch(forceCleanupUrl, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ taskId, force: true })
  })
  .then(response => response.json())
  .then(forceData => {
    console.log('强制清理响应:', forceData);
  })
  .catch(error => {
    console.error('强制清理也失败:', error);
  });
});