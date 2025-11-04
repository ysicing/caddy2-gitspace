# 接口契约：Watcher 接口

**组件**：Kubernetes Watcher
**职责**：监听 Kubernetes Deployment 和 Pod 资源变化，触发路由管理操作

---

## 接口定义

```go
package k8s

import (
    "context"
)

// Watcher 监听 Kubernetes 资源变化
type Watcher interface {
    // Start 启动监听器
    // 阻塞直到 context 取消或发生致命错误
    Start(ctx context.Context) error

    // Stop 停止监听器
    // 优雅关闭所有 informers
    Stop()

    // IsReady 返回监听器是否已准备好（informers已同步）
    IsReady() bool
}

// EventHandler 处理 Kubernetes 事件的回调接口
type EventHandler interface {
    // OnDeploymentAdd 处理 Deployment 创建事件
    // deployment: Deployment 对象
    // 返回 error 如果处理失败
    OnDeploymentAdd(deployment *appsv1.Deployment) error

    // OnDeploymentUpdate 处理 Deployment 更新事件
    // oldDeployment: 更新前的 Deployment
    // newDeployment: 更新后的 Deployment
    OnDeploymentUpdate(oldDeployment, newDeployment *appsv1.Deployment) error

    // OnDeploymentDelete 处理 Deployment 删除事件
    // deployment: 被删除的 Deployment 对象
    OnDeploymentDelete(deployment *appsv1.Deployment) error

    // OnPodUpdate 处理 Pod 更新事件（主要关注就绪状态变化）
    // oldPod: 更新前的 Pod
    // newPod: 更新后的 Pod
    OnPodUpdate(oldPod, newPod *corev1.Pod) error
}
```

---

## 契约测试

### 测试 1：Start 启动成功

**前置条件**：
- 有效的 Kubernetes 配置
- 目标命名空间存在

**操作**：
```go
watcher := NewWatcher(config, handler)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
    err := watcher.Start(ctx)
    assert.NoError(t, err)
}()

// 等待 informer 同步
assert.Eventually(t, func() bool {
    return watcher.IsReady()
}, 5*time.Second, 100*time.Millisecond)
```

**预期结果**：
- `Start()` 无错误返回
- `IsReady()` 返回 true

---

### 测试 2：处理 Deployment 创建事件

**前置条件**：
- Watcher 已启动并就绪
- EventHandler mock 已注册

**操作**：
```go
deployment := &appsv1.Deployment{
    ObjectMeta: metav1.ObjectMeta{
        Name: "test-app",
        Namespace: "default",
        Annotations: map[string]string{
            "gitspace.caddy.default.port": "8080",
        },
    },
    Spec: appsv1.DeploymentSpec{
        Replicas: int32Ptr(1),
    },
}

// 创建 Deployment
_, err := k8sClient.AppsV1().Deployments("default").Create(ctx, deployment, metav1.CreateOptions{})
assert.NoError(t, err)

// 等待事件触发
time.Sleep(500 * time.Millisecond)
```

**预期结果**：
- `EventHandler.OnDeploymentAdd()` 被调用一次
- 参数 `deployment.Name == "test-app"`

---

### 测试 3：处理 Deployment 删除事件

**前置条件**：
- Watcher 已启动
- Deployment 已存在

**操作**：
```go
err := k8sClient.AppsV1().Deployments("default").Delete(ctx, "test-app", metav1.DeleteOptions{})
assert.NoError(t, err)

time.Sleep(500 * time.Millisecond)
```

**预期结果**：
- `EventHandler.OnDeploymentDelete()` 被调用一次

---

### 测试 4：处理 Pod 就绪状态变化

**前置条件**：
- Watcher 已启动
- Pod 初始状态为未就绪

**操作**：
```go
pod := &corev1.Pod{
    ObjectMeta: metav1.ObjectMeta{
        Name: "test-pod",
        Namespace: "default",
    },
    Status: corev1.PodStatus{
        Conditions: []corev1.PodCondition{
            {
                Type: corev1.PodReady,
                Status: corev1.ConditionTrue,  // 变为就绪
            },
        },
        PodIP: "10.0.0.1",
    },
}

_, err := k8sClient.CoreV1().Pods("default").UpdateStatus(ctx, pod, metav1.UpdateOptions{})
assert.NoError(t, err)

time.Sleep(500 * time.Millisecond)
```

**预期结果**：
- `EventHandler.OnPodUpdate()` 被调用
- 参数 `newPod.Status.Conditions[0].Status == ConditionTrue`

---

### 测试 5：Stop 优雅关闭

**前置条件**：
- Watcher 已启动

**操作**：
```go
watcher.Stop()

// 验证 Start() 已退出
select {
case <-ctx.Done():
    // 正常退出
case <-time.After(2 * time.Second):
    t.Fatal("Watcher did not stop in time")
}
```

**预期结果**：
- `Stop()` 在 2 秒内完成
- 所有 goroutines 已清理

---

## 错误场景

### 错误 1：无效的 Kubernetes 配置

**操作**：
```go
config := &Config{
    KubeConfig: "/invalid/path",
}
watcher := NewWatcher(config, handler)
err := watcher.Start(ctx)
```

**预期结果**：
- `Start()` 返回错误
- 错误消息包含 "kubeconfig"

---

### 错误 2：权限不足

**操作**：
```go
// 使用没有权限的 ServiceAccount
err := watcher.Start(ctx)
```

**预期结果**：
- `Start()` 返回错误
- 错误消息包含 "forbidden" 或 "unauthorized"

---

## 性能要求

- **事件延迟**：从 Kubernetes 事件发生到 `EventHandler` 被调用 < 1s
- **内存占用**：Informer 缓存 < 10MB（100 Deployment + 100 Pod）
- **CPU 占用**：空闲时 < 1% CPU

---

## 并发保证

- `Start()` 应该是线程安全的（虽然不推荐多次调用）
- `IsReady()` 可以在任何时候调用，无需锁
- `EventHandler` 回调在单个 goroutine 中串行调用（Informer 保证）
