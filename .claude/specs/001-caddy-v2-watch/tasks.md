# 任务：Caddy v2 Kubernetes Deployment 动态路由扩展

**输入**：来自 `.claude/specs/001-caddy-v2-watch/` 的设计文档
**前提条件**：plan.md（必需）、research.md、data-model.md、contracts/、quickstart.md

## 执行流程（main）
```
1. 从功能目录加载 plan.md
   → 已提取：Go 1.25.3、Caddy v2、Kubernetes client-go
   → 项目结构：单一项目，按功能模块组织
2. 已加载设计文档：
   → research.md：10 个技术决策
   → data-model.md：5 个核心实体
   → contracts/watcher.md：Watcher 接口契约
   → contracts/route-manager.md：RouteManager 接口契约
   → quickstart.md：8 个部署步骤、5 个验收场景
3. 按类别生成任务：
   → 设置：Go 模块、项目结构、依赖
   → 测试：契约测试、集成测试
   → 核心：配置、Watcher、RouteManager
   → 集成：Caddy 模块注册、事件连接
   → 优化：性能测试、文档、部署
4. 应用任务规则：
   → 不同模块/文件 = 标记 [P] 表示可并行
   → 同一模块内 = 顺序执行
   → 测试在实现之前（TDD）
5. 已按顺序编号任务（T001-T030）
6. 已生成依赖关系
7. 已创建并行执行示例
8. 验证任务完整性：✅ 所有契约、实体、场景已覆盖
```

---

## 格式：`[编号] [P?] 描述`
- **[P]**：可以并行运行（不同文件，无依赖关系）
- 在描述中包含确切的文件路径

## 路径约定
本项目使用单一项目结构（来自 plan.md）：
- **源代码**：仓库根目录的 `k8s/`、`router/`、`config/`、`main.go`
- **测试**：`tests/unit/`、`tests/integration/`、`tests/testdata/`
- **部署**：`deployments/`（Kubernetes YAML 清单）

---

## 阶段 3.1：设置 (T001-T004)

### T001 初始化 Go 模块和依赖
**文件**：`go.mod`、`go.sum`
**操作**：
- 运行 `go mod init github.com/ysicing/caddy2-k8s`（如果尚未初始化）
- 添加主要依赖：
  ```bash
  go get github.com/caddyserver/caddy/v2@latest
  go get k8s.io/client-go@latest
  go get k8s.io/api@latest
  go get k8s.io/apimachinery@latest
  go get github.com/stretchr/testify@latest
  ```
- 运行 `go mod tidy`
**验收**：`go build` 无错误

---

### T002 创建项目目录结构
**文件**：目录结构
**操作**：
- 创建目录：
  ```bash
  mkdir -p k8s router config tests/unit tests/integration tests/testdata deployments
  ```
**验收**：所有目录已创建

---

### T003 [P] 创建 .gitignore 和基础配置文件
**文件**：`.gitignore`、`.golangci.yml`（可选）
**操作**：
- 创建 `.gitignore`：
  ```
  # Binaries
  caddy-k8s
  *.exe
  *.dll
  *.so
  *.dylib

  # Test binary
  *.test
  *.out

  # Go workspace
  vendor/

  # IDE
  .idea/
  .vscode/
  *.swp
  *.swo
  *~

  # OS
  .DS_Store
  ```
- （可选）配置 golangci-lint
**验收**：文件已创建

---

### T004 [P] 创建测试数据文件
**文件**：`tests/testdata/deployments.yaml`
**操作**：
- 创建示例 Deployment YAML 用于测试：
  ```yaml
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: test-app
    namespace: default
    annotations:
      gitspace.caddy.default.port: "8080"
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: test-app
    template:
      metadata:
        labels:
          app: test-app
      spec:
        containers:
        - name: app
          image: nginx:alpine
          ports:
          - containerPort: 8080
  ```
**验收**：测试数据文件已创建

---

## 阶段 3.2：测试优先（TDD）⚠️ 必须在 3.3 之前完成 (T005-T012)

**关键：这些测试必须编写并且必须在任何实现之前失败**

### T005 [P] 编写配置模块契约测试
**文件**：`tests/unit/config_test.go`
**操作**：
- 测试配置加载和验证：
  - 有效配置加载成功
  - 缺少必需字段返回错误
  - 端口超范围返回错误
  - 域名格式无效返回错误
**验收**：测试编译成功，运行时全部失败（因为实现尚未存在）

---

### T006 [P] 编写 Watcher 接口契约测试
**文件**：`tests/unit/watcher_test.go`
**操作**：
- 基于 `contracts/watcher.md` 编写测试：
  - 测试 1：Start 启动成功
  - 测试 2：处理 Deployment 创建事件
  - 测试 3：处理 Deployment 删除事件
  - 测试 4：处理 Pod 就绪状态变化
  - 测试 5：Stop 优雅关闭
  - 错误场景：无效 kubeconfig、权限不足
- 使用 fake Kubernetes client 模拟
**验收**：测试编译成功，运行时全部失败

---

### T007 [P] 编写 RouteManager 接口契约测试
**文件**：`tests/unit/manager_test.go`
**操作**：
- 基于 `contracts/route-manager.md` 编写测试：
  - 测试 1-6：AddRoute、GetRouteByDomain、DeleteRoute
  - 测试 7-8：ServeHTTP（路由存在/不存在）
  - 测试 9：ListRoutes
  - 测试 10：并发读写路由表
  - 错误场景：无效参数、目标不可达
**验收**：测试编译成功，运行时全部失败

---

### T008 [P] 编写集成测试：Deployment 创建后自动生成路由
**文件**：`tests/integration/scenario_01_test.go`
**操作**：
- 对应 quickstart.md 场景 1
- 使用 envtest 或 kind 创建测试环境
- 步骤：
  1. 创建 Deployment（带端口注解）
  2. 等待 Pod 就绪
  3. 验证路由已创建
  4. 验证域名注解已写回
**验收**：测试框架搭建完成，测试失败（等待实现）

---

### T009 [P] 编写集成测试：Deployment 删除后自动移除路由
**文件**：`tests/integration/scenario_02_test.go`
**操作**：
- 对应 quickstart.md 场景 2
- 步骤：
  1. 创建 Deployment 和路由
  2. 删除 Deployment
  3. 验证路由已移除
**验收**：测试失败（等待实现）

---

### T010 [P] 编写集成测试：缺少端口注解时使用默认端口
**文件**：`tests/integration/scenario_03_test.go`
**操作**：
- 对应 quickstart.md 场景 3
- 步骤：
  1. 创建 Deployment（无端口注解）
  2. 验证使用默认端口 8089
**验收**：测试失败（等待实现）

---

### T011 [P] 编写集成测试：Pod 重启后路由自动更新
**文件**：`tests/integration/scenario_04_test.go`
**操作**：
- 对应 quickstart.md 场景 4
- 步骤：
  1. 创建 Deployment 和路由
  2. 删除 Pod（模拟重启）
  3. 等待新 Pod 就绪
  4. 验证路由已更新到新 Pod IP
**验收**：测试失败（等待实现）

---

### T012 [P] 编写集成测试：副本数缩减至 0
**文件**：`tests/integration/scenario_05_test.go`
**操作**：
- 对应 quickstart.md 场景 5
- 步骤：
  1. 创建 Deployment 和路由
  2. 缩减副本数至 0
  3. 验证路由已移除
**验收**：测试失败（等待实现）

---

## 阶段 3.3：核心实现（仅在测试失败后）(T013-T021)

### T013 [P] 实现配置模块
**文件**：`config/config.go`
**操作**：
- 定义 Config 结构体：
  ```go
  type Config struct {
      Namespace    string `json:"namespace"`
      BaseDomain   string `json:"base_domain"`
      DefaultPort  int    `json:"default_port,omitempty"`
      KubeConfig   string `json:"kubeconfig,omitempty"`
      ResyncPeriod string `json:"resync_period,omitempty"`
  }
  ```
- 实现 Validate() 方法验证配置
- 实现 Load() 方法从 JSON 加载配置
**依赖**：无
**验收**：T005 测试通过

---

### T014 [P] 实现 Kubernetes 类型定义
**文件**：`k8s/types.go`
**操作**：
- 定义常量：
  ```go
  const (
      AnnotationPort   = "gitspace.caddy.default.port"
      AnnotationURL    = "gitspace.caddy.route.url"
      AnnotationSynced = "gitspace.caddy.route.synced-at"
  )
  ```
- 定义辅助函数：
  - `isPodReady(pod *corev1.Pod) bool`
  - `getPortFromAnnotation(annotations map[string]string, defaultPort int) (int, error)`
**依赖**：无
**验收**：单元测试通过

---

### T015 实现 Kubernetes client 封装
**文件**：`k8s/client.go`
**操作**：
- 创建 Kubernetes clientset
- 支持集群内和集群外配置：
  ```go
  func NewKubernetesClient(kubeconfigPath string) (*kubernetes.Clientset, error)
  ```
- 实现 PATCH Deployment 注解的辅助函数：
  ```go
  func PatchDeploymentAnnotation(client kubernetes.Interface, namespace, name string, annotations map[string]string) error
  ```
**依赖**：T014
**验收**：单元测试通过

---

### T016 [P] 实现路由表数据结构
**文件**：`router/manager.go`（RouteTable 部分）
**操作**：
- 实现 RouteEntry 结构体
- 实现 RouteTable 结构体：
  ```go
  type RouteTable struct {
      byDomain     map[string]*RouteEntry
      byDeployment map[string]*RouteEntry
      mu           sync.RWMutex
  }
  ```
- 实现基本操作：
  - `AddRoute(entry *RouteEntry) error`
  - `DeleteRoute(deploymentKey string) error`
  - `GetRouteByDomain(domain string) (*RouteEntry, bool)`
  - `ListRoutes() []*RouteEntry`
**依赖**：无
**验收**：T007 部分测试通过（路由表操作）

---

### T017 实现 RouteManager HTTP handler
**文件**：`router/handler.go`
**操作**：
- 实现 Caddy HTTP handler 接口：
  ```go
  func (rm *RouteManager) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error
  ```
- 实现反向代理逻辑：
  1. 从请求 Host 头提取域名
  2. 查找路由表获取目标 IP:Port
  3. 反向代理到目标或调用 next handler
**依赖**：T016
**验收**：T007 ServeHTTP 测试通过

---

### T018 实现 Watcher - Informer 创建
**文件**：`k8s/watcher.go`（第 1 部分）
**操作**：
- 实现 Watcher 接口：
  ```go
  type Watcher struct {
      clientset       kubernetes.Interface
      namespace       string
      informerFactory informers.SharedInformerFactory
      eventHandler    EventHandler
  }
  ```
- 实现 Start() 方法：
  1. 创建 SharedInformerFactory
  2. 创建 Deployment 和 Pod Informers
  3. 启动 Informers
**依赖**：T015
**验收**：Informer 创建成功，无崩溃

---

### T019 实现 Watcher - 事件处理器注册
**文件**：`k8s/watcher.go`（第 2 部分）
**操作**：
- 注册 Deployment 事件处理器：
  ```go
  deployInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
      AddFunc:    w.handleDeploymentAdd,
      UpdateFunc: w.handleDeploymentUpdate,
      DeleteFunc: w.handleDeploymentDelete,
  })
  ```
- 注册 Pod 事件处理器：
  ```go
  podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
      UpdateFunc: w.handlePodUpdate,
  })
  ```
**依赖**：T018
**验收**：事件处理器注册成功

---

### T020 实现 Watcher - Deployment 事件处理逻辑
**文件**：`k8s/watcher.go`（第 3 部分）
**操作**：
- 实现 handleDeploymentAdd：
  1. 读取端口注解
  2. 查找就绪的 Pod
  3. 如果 Pod 就绪，调用 EventHandler.OnDeploymentAdd
- 实现 handleDeploymentUpdate：
  1. 检查副本数是否变为 0
  2. 调用相应的 EventHandler 方法
- 实现 handleDeploymentDelete：
  1. 调用 EventHandler.OnDeploymentDelete
**依赖**：T019、T014
**验收**：T006 测试通过

---

### T021 实现 Watcher - Pod 事件处理逻辑
**文件**：`k8s/watcher.go`（第 4 部分）
**操作**：
- 实现 handlePodUpdate：
  1. 检查 Pod 就绪状态变化
  2. 获取所属 Deployment
  3. 调用 EventHandler.OnPodUpdate
**依赖**：T020
**验收**：T006 测试通过（Pod 就绪状态变化）

---

## 阶段 3.4：集成 (T022-T025)

### T022 实现 Caddy 模块注册和配置
**文件**：`router/config.go`
**操作**：
- 实现 Caddy Module 接口：
  ```go
  func (K8sRouter) CaddyModule() caddy.ModuleInfo {
      return caddy.ModuleInfo{
          ID:  "http.handlers.k8s_router",
          New: func() caddy.Module { return new(K8sRouter) },
      }
  }
  ```
- 实现 Provision 接口加载配置
- 实现 Validate 接口验证配置
**依赖**：T013、T016
**验收**：Caddy 模块注册成功

---

### T023 实现 EventHandler 连接 Watcher 和 RouteManager
**文件**：`main.go`（EventHandler 实现）
**操作**：
- 实现 EventHandler 接口：
  ```go
  type K8sEventHandler struct {
      routeManager *router.RouteManager
      k8sClient    kubernetes.Interface
      namespace    string
      baseDomain   string
  }
  ```
- 实现所有事件处理方法：
  - OnDeploymentAdd：创建路由 + 写回注解
  - OnDeploymentUpdate：检查副本数变化
  - OnDeploymentDelete：删除路由
  - OnPodUpdate：更新路由（Pod IP 变化）
**依赖**：T020、T021、T017、T015
**验收**：事件正确触发路由操作

---

### T024 实现主入口和 Caddy 模块初始化
**文件**：`main.go`
**操作**：
- 创建 K8sRouter 结构体：
  ```go
  type K8sRouter struct {
      Config       *config.Config
      RouteManager *router.RouteManager
      Watcher      *k8s.Watcher
      ctx          context.Context
      cancel       context.CancelFunc
  }
  ```
- 实现 Provision：
  1. 初始化 Kubernetes client
  2. 创建 RouteManager
  3. 创建 Watcher 和 EventHandler
  4. 启动 Watcher（在 goroutine 中）
- 实现 Cleanup：优雅关闭 Watcher
**依赖**：T022、T023
**验收**：Caddy 启动成功，扩展已加载

---

### T025 实现注解写回逻辑
**文件**：已在 T023 的 EventHandler 中实现
**操作**：
- 在路由创建成功后，调用 PatchDeploymentAnnotation：
  ```go
  annotations := map[string]string{
      k8s.AnnotationURL:    domain,
      k8s.AnnotationSynced: time.Now().Format(time.RFC3339),
  }
  k8s.PatchDeploymentAnnotation(client, namespace, name, annotations)
  ```
**依赖**：T015、T023
**验收**：Deployment 注解已正确写回

---

## 阶段 3.5：优化和文档 (T026-T030)

### T026 [P] 编写性能测试
**文件**：`tests/integration/performance_test.go`
**操作**：
- 测试 100 Deployment 并发创建：
  - 并发创建 100 个 Deployment
  - 测量路由创建总时间
  - 验证所有路由正确创建
- 测试路由查找性能：
  - 创建 1000+ 路由
  - 测量 GetRouteByDomain 延迟
  - 验证 < 100μs
**依赖**：T024
**验收**：性能测试通过，满足目标

---

### T027 [P] 创建 Kubernetes 部署清单
**文件**：`deployments/rbac.yaml`、`deployments/deployment.yaml`、`deployments/service.yaml`、`deployments/configmap.yaml`
**操作**：
- 创建 RBAC 资源（ServiceAccount、Role、RoleBinding）
- 创建 Caddy Deployment YAML
- 创建 Service（LoadBalancer 类型）
- 创建 ConfigMap（Caddy 配置）
**依赖**：无（可并行）
**验收**：YAML 清单验证通过（kubectl apply --dry-run）

---

### T028 [P] 创建 Dockerfile
**文件**：`Dockerfile`
**操作**：
- 多阶段构建：
  ```dockerfile
  FROM golang:1.25.3-alpine AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -o caddy-k8s .

  FROM alpine:latest
  RUN apk --no-cache add ca-certificates
  WORKDIR /root/
  COPY --from=builder /app/caddy-k8s .
  EXPOSE 80 443
  CMD ["./caddy-k8s", "run", "--config", "/etc/caddy/config.json"]
  ```
**依赖**：无（可并行）
**验收**：Docker 镜像构建成功

---

### T029 [P] 编写 README.md
**文件**：`README.md`
**操作**：
- 项目简介
- 功能特性
- 快速开始（引用 quickstart.md）
- 配置说明
- 开发指南
- 许可证
**依赖**：无（可并行）
**验收**：README 内容完整

---

### T030 验证 quickstart.md 可执行性
**文件**：无（验证任务）
**操作**：
- 按照 quickstart.md 步骤 1-8 执行
- 使用 kind 创建本地测试集群
- 部署 Caddy 扩展
- 运行所有验收场景
- 记录执行结果和任何问题
**依赖**：T024、T027、T028
**验收**：所有场景通过，quickstart 可执行

---

## 依赖关系

**设置阶段**：
- T001-T004 无依赖，可并行（除 T001 必须最先）

**测试阶段**：
- T005-T012 可并行（测试优先，TDD）

**实现阶段**：
- T013：无依赖 [P]
- T014：无依赖 [P]
- T015：依赖 T014
- T016：无依赖 [P]
- T017：依赖 T016
- T018：依赖 T015
- T019：依赖 T018
- T020：依赖 T019、T014
- T021：依赖 T020

**集成阶段**：
- T022：依赖 T013、T016
- T023：依赖 T020、T021、T017、T015
- T024：依赖 T022、T023
- T025：依赖 T015、T023

**优化阶段**：
- T026：依赖 T024 [P]
- T027：无依赖 [P]
- T028：无依赖 [P]
- T029：无依赖 [P]
- T030：依赖 T024、T027、T028

---

## 并行执行示例

### 示例 1：设置阶段（T002-T004）
```bash
# T001 必须先完成（初始化 go.mod）
# 然后可以并行执行：
task T002 &  # 创建目录结构
task T003 &  # 创建配置文件
task T004 &  # 创建测试数据
wait
```

### 示例 2：测试阶段（T005-T012）
所有测试任务可以并行执行：
```bash
task T005 &  # 配置模块测试
task T006 &  # Watcher 测试
task T007 &  # RouteManager 测试
task T008 &  # 集成测试 1
task T009 &  # 集成测试 2
task T010 &  # 集成测试 3
task T011 &  # 集成测试 4
task T012 &  # 集成测试 5
wait
```

### 示例 3：核心实现第一波（T013、T014、T016）
```bash
task T013 &  # 配置模块
task T014 &  # Kubernetes 类型定义
task T016 &  # 路由表
wait
# 然后继续 T015（依赖 T014）
```

### 示例 4：优化阶段（T026-T029）
```bash
task T026 &  # 性能测试
task T027 &  # K8s 清单
task T028 &  # Dockerfile
task T029 &  # README
wait
# 然后 T030（验证 quickstart）
```

---

## 注意事项

- **[P] 任务**：标记为 [P] 的任务操作不同的文件，可以安全并行执行
- **验证测试失败**：在实现之前，确保 T005-T012 的测试已编写并失败
- **每个任务后提交**：建议每完成一个任务提交一次 git commit
- **避免同文件冲突**：没有两个 [P] 任务修改相同文件

---

## 验证清单

- [x] 所有契约都有对应的测试（T006、T007）
- [x] 所有实体都有模型任务（RouteEntry、RouteTable：T016）
- [x] 所有测试都在实现之前（T005-T012 在 T013-T021 之前）
- [x] 并行任务真正独立（已验证文件不冲突）
- [x] 每个任务指定确切的文件路径（已包含）
- [x] 没有任务修改与另一个 [P] 任务相同的文件（已验证）

---

## 总计

- **总任务数**：30 个
- **可并行任务**：15 个（标记 [P]）
- **预计串行执行时间**：假设每个任务 30 分钟，约 15 小时
- **预计并行执行时间**：约 8-10 小时（利用并行优势）

---

**准备开始**：所有任务已准备好按顺序执行。建议使用 `/spec-kit:implement` 或手动逐个完成任务。
