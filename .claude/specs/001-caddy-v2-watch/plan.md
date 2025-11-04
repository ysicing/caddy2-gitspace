# 实施计划：Caddy v2 Kubernetes Deployment 动态路由扩展

**分支**：`001-caddy-v2-watch` | **日期**：2025-11-04 | **规格**：[spec.md](./spec.md)
**输入**：来自 `.claude/specs/001-caddy-v2-watch/spec.md` 的功能规格

## 执行流程（/spec-kit:plan 命令范围）
```
1. 从输入路径加载功能规格
   → 如果未找到：错误 "路径 {path} 下没有功能规格"
2. 填充技术上下文（扫描需要澄清的内容）
   → 从文件系统结构或上下文检测项目类型（web=前端+后端，mobile=应用+API）
   → 根据项目类型设置结构决策
3. 根据章程文档内容填充章程检查部分
4. 评估下面的章程检查部分
   → 如果存在违规：在复杂性跟踪中记录
   → 如果无法提供理由：错误 "请先简化方法"
   → 更新进度跟踪：初始章程检查
5. 执行阶段 0 → research.md
   → 如果仍有需要澄清的内容：错误 "解决未知项"
6. 执行阶段 1 → contracts、data-model.md、quickstart.md、代理特定模板文件（例如，Claude Code 的 `CLAUDE.md`、GitHub Copilot 的 `.github/copilot-instructions.md`、Gemini CLI 的 `GEMINI.md`、Qwen Code 的 `QWEN.md` 或 opencode 的 `AGENTS.md`）
7. 重新评估章程检查部分
   → 如果有新违规：重构设计，返回阶段 1
   → 更新进度跟踪：设计后章程检查
8. 规划阶段 2 → 描述任务生成方法（不要创建 tasks.md）
9. 停止 - 准备执行 /spec-kit:tasks 命令
```

**重要**：/spec-kit:plan 命令在步骤 7 停止。阶段 2-4 由其他命令执行：
- 阶段 2：/spec-kit:tasks 命令创建 tasks.md
- 阶段 3-4：实施执行（手动或通过工具）

## 摘要
实现一个 Caddy v2 扩展，通过监听指定 Kubernetes 命名空间中的 Deployment 资源，自动创建和管理 HTTP 反向代理路由。当 Deployment 创建且 Pod 就绪时，扩展自动生成域名路由（格式：{deployment-name}.{base-domain}）并转发到 Pod IP。当 Deployment 删除或副本缩减至 0 时，自动清理路由。支持通过注解配置端口，并将生成的域名信息写回 Deployment。

## 技术上下文
**语言/版本**：Go 1.25.3
**主要依赖**：
- Caddy v2 (github.com/caddyserver/caddy/v2)
- Kubernetes Client-go (k8s.io/client-go)
- Kubernetes API Machinery (k8s.io/apimachinery)
**存储**：内存状态（路由映射缓存），Kubernetes API 作为真实数据源
**测试**：Go testing 标准库 + testify
**目标平台**：Linux 服务器（容器化部署）
**项目类型**：single（单一 Caddy 扩展项目）
**性能目标**：
- Watch 事件响应延迟 <1s
- 路由更新延迟 <500ms
- 支持单命名空间内 100+ Deployment
**约束**：
- 只监听单个命名空间
- 所有 Deployment 假设为单副本
- 需要 Kubernetes RBAC 权限（读 Deployment/Pod，写 Deployment 注解）
**规模/范围**：小型扩展（预计 <1000 LOC），单一功能模块

## 章程检查
*门禁：必须在阶段 0 研究之前通过。在阶段 1 设计后重新检查。*

由于项目中不存在 `.claude/memory/constitution.md` 文件，将应用通用最佳实践原则：

### 简单性原则
- [x] 单一职责：扩展只负责 Deployment 到路由的映射
- [x] 最小依赖：只使用必要的 Caddy 和 Kubernetes 库
- [x] 避免过度设计：不引入不必要的抽象层

### 可测试性原则
- [x] 关注点分离：K8s 监听逻辑与 Caddy 路由逻辑分离
- [x] 可模拟接口：Kubernetes client 可通过接口模拟
- [x] 契约测试：核心功能需求可通过单元测试验证

### 可维护性原则
- [x] 清晰命名：函数和变量名称反映其用途
- [x] 文档化配置：所有配置项需要明确文档
- [x] 错误处理：所有错误路径都需要适当处理和日志记录

## 项目结构

### 文档（此功能）
```
specs/[###-feature]/
├── plan.md              # 此文件（/spec-kit:plan 命令输出）
├── research.md          # 阶段 0 输出（/spec-kit:plan 命令）
├── data-model.md        # 阶段 1 输出（/spec-kit:plan 命令）
├── quickstart.md        # 阶段 1 输出（/spec-kit:plan 命令）
├── contracts/           # 阶段 1 输出（/spec-kit:plan 命令）
└── tasks.md             # 阶段 2 输出（/spec-kit:tasks 命令 - 不由 /spec-kit:plan 创建）
```

### 源代码（仓库根目录）
```
caddy2-k8s/
├── main.go                  # Caddy 扩展主入口
├── k8s/
│   ├── watcher.go          # Kubernetes Deployment/Pod 监听器
│   ├── client.go           # Kubernetes client 封装
│   └── types.go            # 类型定义和常量
├── router/
│   ├── manager.go          # 路由管理器（添加/删除路由）
│   ├── config.go           # Caddy 配置结构
│   └── handler.go          # HTTP handler 实现
├── config/
│   └── config.go           # 配置加载和验证
└── tests/
    ├── integration/        # 集成测试（需要 K8s 环境）
    │   └── e2e_test.go
    ├── unit/               # 单元测试
    │   ├── watcher_test.go
    │   ├── manager_test.go
    │   └── config_test.go
    └── testdata/           # 测试数据（模拟 K8s 对象）
        └── deployments.yaml
```

**结构决策**：采用单一项目结构（选项 1），按功能模块组织（k8s/、router/、config/）。选择理由：
- 这是一个单一 Caddy 扩展，不需要前后端分离
- 功能模块清晰：K8s 交互、路由管理、配置处理
- 测试与源代码分离，便于维护

## 阶段 0：概述与研究
1. **从上面的技术上下文中提取未知项**：
   - 对于每个需要澄清的内容 → 研究任务
   - 对于每个依赖 → 最佳实践任务
   - 对于每个集成 → 模式任务

2. **生成并派发研究代理**：
   ```
   对于技术上下文中的每个未知项：
     任务："研究 {未知项} 用于 {功能上下文}"
   对于每个技术选择：
     任务："查找 {领域} 中 {技术} 的最佳实践"
   ```

3. **在 `research.md` 中合并发现**，使用格式：
   - 决策：[选择了什么]
   - 理由：[为什么选择]
   - 考虑的替代方案：[还评估了什么]

**输出**：research.md，所有需要澄清的内容都已解决

## 阶段 1：设计与契约
*前提条件：research.md 完成*

1. **从功能规格中提取实体** → `data-model.md`：
   - 实体名称、字段、关系
   - 来自需求的验证规则
   - 如适用的状态转换

2. **从功能需求生成 API 契约**：
   - 对于每个用户操作 → 端点
   - 使用标准 REST/GraphQL 模式
   - 输出 OpenAPI/GraphQL 模式到 `/contracts/`

3. **从契约生成契约测试**：
   - 每个端点一个测试文件
   - 断言请求/响应模式
   - 测试必须失败（尚无实现）

4. **从用户故事中提取测试场景**：
   - 每个故事 → 集成测试场景
   - 快速启动测试 = 故事验证步骤

5. **增量更新代理文件**（O(1) 操作）：
   - 运行 `.specify/scripts/bash/update-agent-context.sh claude`
     **重要**：按上述指定执行。不要添加或删除任何参数。
   - 如果存在：仅从当前计划添加新技术
   - 在标记之间保留手动添加
   - 更新最近变更（保留最后 3 个）
   - 保持在 150 行以下以提高令牌效率
   - 输出到仓库根目录

**输出**：data-model.md、/contracts/*、失败的测试、quickstart.md、代理特定文件

## 阶段 2：任务规划方法
*本节描述 /spec-kit:tasks 命令将执行的操作 - 不要在 /spec-kit:plan 期间执行*

**任务生成策略**：
/spec-kit:tasks 命令将从阶段 1 的设计文档生成任务列表，按以下策略组织：

### 任务类别

1. **基础设施任务** [P]
   - 初始化 Go 模块和依赖
   - 创建项目目录结构（k8s/、router/、config/、tests/）
   - 配置 Docker 构建环境

2. **配置模块任务** [P]
   - 实现 config.go（配置结构和验证）
   - 单元测试：配置加载和验证逻辑

3. **Kubernetes 监听模块任务**
   - 实现 k8s/types.go（类型定义和常量）
   - 实现 k8s/client.go（Kubernetes client 封装）[P]
   - 实现 k8s/watcher.go（Informer 监听器）
   - 单元测试：Watcher 事件处理逻辑（mock client）
   - 契约测试：验证 Watcher 接口契约

4. **路由管理模块任务**
   - 实现 router/manager.go（RouteTable 和路由操作）[P]
   - 实现 router/config.go（Caddy 配置结构）
   - 实现 router/handler.go（HTTP handler 和反向代理）
   - 单元测试：RouteManager 并发安全测试
   - 单元测试：路由查找性能测试
   - 契约测试：验证 RouteManager 接口契约

5. **集成任务**
   - 实现 main.go（Caddy 模块注册和入口）
   - 连接 Watcher 和 RouteManager（事件回调实现）
   - 实现注解写回逻辑（PATCH Deployment）

6. **测试任务**
   - 集成测试：端到端测试（使用 kind 创建测试集群）
   - 集成测试：验收场景 1-5（对应 spec.md 中的场景）
   - 性能测试：100 Deployment 并发创建
   - 性能测试：路由查找性能（1000+ 路由）

7. **文档和部署任务** [P]
   - 编写 README.md
   - 创建 Kubernetes YAML 清单（RBAC、Deployment、Service）
   - 创建 Dockerfile
   - 验证 quickstart.md 可执行性

**排序策略**：
- **依赖顺序**：
  1. 基础设施 → 配置模块
  2. 配置模块 → Kubernetes 模块 + 路由模块（可并行）
  3. Kubernetes 模块 + 路由模块 → 集成
  4. 集成 → 测试
  5. 测试 → 文档和部署

- **TDD 顺序**：每个模块的单元测试在实现完成后立即编写
- **并行标记 [P]**：标记为 [P] 的任务可以并行执行（无依赖）

**预计任务数量**：25-30 个任务

**任务格式**：
每个任务包含：
- 任务编号（T-001, T-002, ...）
- 任务描述（动词开头，如"实现"、"测试"、"验证"）
- 依赖任务（如果有）
- 并行标记 [P]（如果适用）
- 验收标准（如何判断任务完成）

**示例任务**：
```
T-001: [P] 初始化 Go 模块
  依赖：无
  操作：
    - 运行 go mod init
    - 添加必要依赖（Caddy v2、client-go）
    - 运行 go mod tidy
  验收：go build 无错误

T-010: 实现 k8s/watcher.go
  依赖：T-008（k8s/client.go）
  操作：
    - 实现 Watcher 接口
    - 创建 SharedInformerFactory
    - 注册 Deployment 和 Pod 事件处理器
  验收：单元测试通过，契约测试通过
```

**重要**：此阶段由 /spec-kit:tasks 命令执行，而不是由 /spec-kit:plan 执行

## 阶段 3+：未来实施
*这些阶段超出了 /spec-kit:plan 命令的范围*

**阶段 3**：任务执行（/spec-kit:tasks 命令创建 tasks.md）
**阶段 4**：实施（按照章程原则执行 tasks.md）
**阶段 5**：验证（运行测试、执行 quickstart.md、性能验证）

## 复杂性跟踪
*仅在章程检查有必须证明合理的违规时填写*

无复杂性违规。本项目遵循所有简单性和可维护性原则：
- 单一职责：每个模块功能明确
- 最小依赖：只使用必要的库
- 无过度抽象：直接使用 Kubernetes client-go 和 Caddy API


## 进度跟踪
*此清单在执行流程期间更新*

**阶段状态**：
- [x] 阶段 0：研究完成（/spec-kit:plan 命令）
- [x] 阶段 1：设计完成（/spec-kit:plan 命令）
- [x] 阶段 2：任务规划完成（/spec-kit:plan 命令 - 仅描述方法）
- [x] 阶段 3：任务已生成（/spec-kit:tasks 命令）
- [ ] 阶段 4：实施完成
- [ ] 阶段 5：验证通过

**门禁状态**：
- [x] 初始章程检查：通过
- [x] 设计后章程检查：通过
- [x] 所有需要澄清的内容已解决
- [x] 复杂性偏差已记录

**生成的文档**：
- [x] research.md - 技术研究和决策
- [x] data-model.md - 数据模型定义
- [x] contracts/watcher.md - Watcher 接口契约
- [x] contracts/route-manager.md - RouteManager 接口契约
- [x] quickstart.md - 快速开始指南
- [x] tasks.md - 30 个详细任务列表

**下一步**：
开始执行 tasks.md 中的任务，从 T001（初始化 Go 模块）开始。

---
*基于 spec-kit 工作流 - 参见功能规格 spec.md*