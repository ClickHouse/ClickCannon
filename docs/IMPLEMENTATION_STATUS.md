# 配置化数据生成 - 实现状态

## 📊 当前状态

### ✅ 已完成部分

1. **配置文件设计** (100%)
   - ✅ `dns_logs_profile.yaml` - 70+ DNS 字段配置
   - ✅ `custom_logs_example.yaml` - 示例配置
   - ✅ `quickstart.yaml` - 快速入门配置
   - ✅ 支持 12 种生成器类型定义

2. **文档完成** (100%)
   - ✅ 数据生成流程与自定义配置指南
   - ✅ DNS 日志生成使用指南
   - ✅ 方案总结文档
   - ✅ 快速开始指南
   - ✅ 数组字段临时解决方案

3. **运行配置** (100%)
   - ✅ `config_dns_logs.yaml` - DNS 日志配置
   - ✅ Disk 模式配置（可用）
   - ✅ 性能参数配置

### ⏳ 待实现部分

1. **代码实现** (0%)
   - ❌ 配置文件解析器 (`custom_config.go`)
   - ❌ 动态列管理器 (`dynamic_columns.go`)
   - ❌ 生成器工厂 (`generator_factory.go`)
   - ❌ Scheduler 集成
   - ❌ Worker 适配

2. **高级功能** (0%)
   - ❌ 数组类型支持
   - ❌ 条件生成器
   - ❌ 字段引用
   - ❌ 模板生成器

## 🚀 可用方案

### 方案 1: Disk 模式 (立即可用) ✅

**状态**: 已配置完成，可直接使用

```bash
# 使用现有数据文件重放
./clickcannon -config config_dns_logs.yaml
```

**配置位置**: `config_dns_logs.yaml` (已设置为 disk 模式)

**优点**:
- ✅ 立即可用，无需开发
- ✅ 使用真实数据结构
- ✅ 支持所有字段（包括数组）
- ✅ 性能优秀

**缺点**:
- ❌ 需要预先导出的数据文件
- ❌ 数据分布固定（来自原始数据）
- ❌ 无法动态调整字段内容

**适用场景**:
- 压力测试
- 性能基准测试
- 稳定性测试
- 需要真实数据特征的场景

### 方案 2: Generate 模式 (需要开发) ⏳

**状态**: 配置文件已就绪，代码实现待完成

**预计开发时间**:
- Phase 1 (基础功能): 2-3 小时
- Phase 2 (完整功能): 4-6 小时

**开发文档**: 见 `GENERATE_MODE_FIX.md`

**优点**:
- ✅ 完全配置化，零代码修改
- ✅ 灵活调整数据分布
- ✅ 支持自定义字段规则
- ✅ 适合各种测试场景

**缺点**:
- ❌ 需要开发时间
- ❌ 初期不支持复杂类型（数组）

**适用场景**:
- 需要频繁调整数据分布
- 测试不同业务场景
- 快速迭代测试
- 非开发人员使用

## 📋 实现路线图

### Phase 1: 基础配置化支持 (2-3 小时)

**目标**: 支持基本类型字段的配置化生成

**任务清单**:
- [ ] 创建 `internal/generate/custom_config.go`
  - [ ] 实现 `CustomFieldsConfig` 结构
  - [ ] 实现 `LoadCustomFieldsConfig()` 函数
  - [ ] 支持 YAML 解析

- [ ] 创建 `internal/generate/dynamic_columns.go`
  - [ ] 实现 `DynamicColumns` 结构
  - [ ] 实现列类型映射
  - [ ] 实现 Fill() 方法

- [ ] 创建 `internal/generate/generator_factory.go`
  - [ ] 实现 `BuildGeneratorFromConfig()`
  - [ ] 支持基础生成器: const, pool, uuid, ip_v4, int_range, hex, random_string

- [ ] 修改 `internal/generate/config.go`
  - [ ] 添加 `EnableCustomFields` 字段
  - [ ] 添加 `ProfileConfigFile` 字段

- [ ] 修改 `internal/generate/scheduler.go`
  - [ ] 集成配置文件加载
  - [ ] 使用动态 Filler

- [ ] 测试验证
  - [ ] 单元测试
  - [ ] 集成测试
  - [ ] DNS 日志表测试

**交付物**:
- ✅ 支持 `dns_logs_profile.yaml` 基础字段生成
- ✅ 支持 70+ 非数组字段
- ✅ 性能与硬编码方式相当

### Phase 2: 高级功能 (4-6 小时)

**目标**: 支持复杂类型和高级生成器

**任务清单**:
- [ ] 数组类型支持
  - [ ] Array(String)
  - [ ] Array(LowCardinality(UInt16))
  - [ ] Array(UInt32)

- [ ] Map 类型支持
  - [ ] Map(String, String)
  - [ ] 支持概率性键值对

- [ ] 条件生成器
  - [ ] 字段引用
  - [ ] 条件表达式
  - [ ] if-then-else 逻辑

- [ ] 模板生成器
  - [ ] 字符串模板
  - [ ] 字段组合

- [ ] 权重优化
  - [ ] 加权随机选择
  - [ ] 分布验证

**交付物**:
- ✅ 完整支持 DNS 日志所有字段
- ✅ 支持复杂的生成规则
- ✅ 配置文件完全功能

### Phase 3: 生产优化 (待定)

- [ ] 性能优化
- [ ] 配置验证工具
- [ ] 自动表结构生成
- [ ] Web UI 配置编辑器

## 🔍 当前运行方式

### 使用 Disk 模式 (推荐)

```bash
# 1. 确认配置
cat config_dns_logs.yaml  # 确认 disk.enabled: true

# 2. 检查数据
ls log_data/*.native*

# 3. 运行
./clickcannon -config config_dns_logs.yaml

# 4. 监控
watch -n 1 "clickhouse-client -q 'SELECT count() FROM dnsmon.cdns_log'"
```

### 切换到 Generate 模式 (实现后)

```bash
# 1. 修改配置
vim config_dns_logs.yaml
# 改为: generate.enabled: true

# 2. 运行
./clickcannon -config config_dns_logs.yaml

# 3. 验证配置加载
# 日志应显示: "loaded custom fields" count=70
```

## 📈 实现优先级

### 高优先级 (立即)
1. ✅ Disk 模式可用性验证
2. ⏳ Phase 1 基础配置化实现

### 中优先级 (短期)
1. ⏳ Phase 2 数组类型支持
2. ⏳ 配置验证工具

### 低优先级 (长期)
1. ⏳ Web UI 编辑器
2. ⏳ 自动化工具

## 💡 建议

### 立即行动
1. 使用 Disk 模式进行测试（已配置）
2. 验证数据写入和查询性能
3. 确定是否需要 Generate 模式

### 如需 Generate 模式
1. 评估开发资源（2-3 小时）
2. 按照 `GENERATE_MODE_FIX.md` 实现
3. 验证 DNS 字段配置

### 长期规划
1. 收集实际使用反馈
2. 优化配置文件格式
3. 扩展生成器类型
4. 构建配置工具链

## 📞 支持

### 文档资源
- **实现指南**: GENERATE_MODE_FIX.md
- **使用指南**: docs/DNS日志生成使用指南.md
- **配置示例**: profiles/dns_logs_profile.yaml
- **快速开始**: QUICK_START_DNS.md

### 当前可用
- ✅ Disk 模式完整配置
- ✅ 所有配置文件模板
- ✅ 完整的使用文档
- ✅ 代码实现指导

### 待开发
- ⏳ Generate 模式代码
- ⏳ 配置解析器
- ⏳ 动态列管理

---

**更新时间**: 2026-08-10  
**状态**: Disk 模式可用，Generate 模式待实现  
**下一步**: 根据需求决定是否实现 Phase 1
