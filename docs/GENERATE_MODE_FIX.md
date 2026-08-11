# Generate 模式配置化支持 - 实现说明

## 问题分析

当前错误:
```
NO_SUCH_COLUMN_IN_TABLE: No such column Timestamp in table dnsmon.cdns_log
```

**根本原因**: 
- Generate 模式的代码硬编码了 OTel 的列结构（Timestamp, TraceId 等）
- 但 DNS 表用的是不同的列名（tnow, serverAddress 等）
- `dns_logs_profile.yaml` 配置文件目前还没有被代码读取和使用

## 当前状态

### 已完成 ✅
- ✅ DNS 配置文件设计完成 (`dns_logs_profile.yaml`)
- ✅ 70+ 字段的生成规则定义完成
- ✅ 文档和使用指南完成

### 未完成 ❌
- ❌ 代码层面的配置化支持（需要实现 Phase 1）
- ❌ 动态列管理器
- ❌ 配置文件解析器
- ❌ 生成器工厂

## 临时解决方案

### 方案 A: 使用 Disk 模式（立即可用）

```yaml
# config_dns_logs.yaml
disk:
  enabled: true
  threads: 16
  logs_path: log_data
  loop: true
  shift_timestamp: now
```

**优点**:
- 立即可用，无需修改代码
- 使用真实的 DNS 数据结构
- 支持所有字段包括数组

**缺点**:
- 需要预先导出的数据文件
- 数据分布固定

### 方案 B: 实现配置化支持（需要开发）

需要实现以下模块:

#### 1. 配置文件解析器

创建 `internal/generate/custom_config.go`:

```go
package generate

type CustomFieldsConfig struct {
    Version      string                  `yaml:"version"`
    DataType     string                  `yaml:"data_type"`
    CustomFields map[string]FieldConfig  `yaml:"custom_fields"`
}

type FieldConfig struct {
    Type           string          `yaml:"type"`
    ClickHouseType string          `yaml:"clickhouse_type"`
    Generator      GeneratorConfig `yaml:"generator"`
}

func LoadCustomFieldsConfig(path string) (*CustomFieldsConfig, error) {
    // 读取并解析 YAML 文件
}
```

#### 2. 动态列管理器

创建 `internal/generate/dynamic_columns.go`:

```go
package generate

type DynamicColumns struct {
    fields      []string
    columns     []proto.Column
    generators  []Gen
    cachedInput proto.Input
}

func NewDynamicColumns(config *CustomFieldsConfig) *DynamicColumns {
    dc := &DynamicColumns{
        fields:     make([]string, 0, len(config.CustomFields)),
        columns:    make([]proto.Column, 0, len(config.CustomFields)),
        generators: make([]Gen, 0, len(config.CustomFields)),
    }
    
    for name, fieldCfg := range config.CustomFields {
        // 创建列
        col := createColumnForType(fieldCfg.Type, fieldCfg.ClickHouseType)
        dc.columns = append(dc.columns, col)
        
        // 创建生成器
        gen, _ := BuildGeneratorFromConfig(fieldCfg.Generator)
        dc.generators = append(dc.generators, gen)
        
        dc.fields = append(dc.fields, name)
    }
    
    // 构建 Input
    dc.rebuildInput()
    return dc
}

func (dc *DynamicColumns) Fill(ctx context.Context, rng *Rng, n int) int {
    for i := 0; i < n; i++ {
        for j, gen := range dc.generators {
            value := gen.Generate(rng)
            dc.appendToColumn(j, value)
        }
    }
    return n
}
```

#### 3. 生成器工厂

创建 `internal/generate/generator_factory.go`:

```go
package generate

func BuildGeneratorFromConfig(cfg GeneratorConfig) (Gen, error) {
    switch cfg.Type {
    case "const":
        return Const(cfg.Value), nil
    case "pool":
        return Pool(cfg.Values...), nil
    case "uuid":
        return UUID(), nil
    case "ip_v4":
        return IP(), nil
    case "int_range":
        return Int(cfg.Min, cfg.Max), nil
    // ... 更多生成器
    }
}
```

#### 4. 集成到 Scheduler

修改 `internal/generate/scheduler.go`:

```go
func (s *Scheduler) Run(ctx context.Context) error {
    var customConfig *CustomFieldsConfig
    
    // 加载自定义配置
    if s.cfg.EnableCustomFields && s.cfg.ProfileConfigFile != "" {
        customConfig, err = LoadCustomFieldsConfig(s.cfg.ProfileConfigFile)
        if err != nil {
            return err
        }
    }
    
    // 创建动态 Filler
    var filler Filler
    if customConfig != nil {
        filler = NewDynamicFiller(customConfig)
    } else {
        // 使用现有的 Profile 方式
        profile, _ := GetProfile(profileName)
        filler = NewLogsFiller(profile)
    }
    
    // 创建 Workers...
}
```

## 实现步骤

### Phase 1: 基础支持（预计 2-3 小时）

```bash
# 1. 创建配置解析
touch internal/generate/custom_config.go
# 实现: LoadCustomFieldsConfig(), FieldConfig 等

# 2. 创建动态列
touch internal/generate/dynamic_columns.go
# 实现: DynamicColumns, createColumnForType() 等

# 3. 创建生成器工厂
touch internal/generate/generator_factory.go
# 实现: BuildGeneratorFromConfig() 支持基础类型

# 4. 修改 Config
# 在 internal/generate/config.go 添加:
# EnableCustomFields bool
# ProfileConfigFile string

# 5. 集成到 Scheduler
# 修改 scheduler.go 使用动态列

# 6. 编译测试
go build -o clickcannon
./clickcannon -config config_dns_logs.yaml
```

### Phase 2: 完整功能（预计 4-6 小时）

- Map 类型支持
- 数组类型支持
- 条件生成器
- 字段引用

## 快速修复建议

### 选项 1: 使用现有数据（推荐）

```bash
# 1. 确认你有导出的 DNS 数据
ls log_data/*.native*

# 2. 使用 disk 模式
./clickcannon -config config_dns_logs.yaml
```

### 选项 2: 创建代码映射

创建一个专门的 DNS Profile (代码方式):

```go
// internal/generate/profile_dns.go
package generate

func init() { RegisterProfile("dns", newProfileDns) }

func newProfileDns() *Profile {
    return NewProfile("dns").
        WithTnow(Timestamp()).
        WithServerAddress(Pool("10.160.152.186", "10.160.152.187")).
        WithClientAddress(IP()).
        WithFirstQueryName(Pool("www.baidu.com", "www.google.com")).
        // ... 70+ 字段
}
```

然后修改 columns 定义适配 DNS 表结构。

### 选项 3: 等待完整实现

如果时间允许，实现完整的配置化支持（Phase 1），预计需要 2-3 小时开发时间。

## 验证步骤

实现后验证:

```bash
# 1. 编译
go build -o clickcannon

# 2. 测试配置加载
./clickcannon -config config_dns_logs.yaml

# 3. 检查日志
# 应该看到: "loaded custom fields" count=70

# 4. 验证数据
clickhouse-client -q "
SELECT count(), uniq(serverAddress)
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 MINUTE
"
```

## 文件清单

需要创建/修改的文件:

```
internal/generate/
├── custom_config.go      (新建) - 配置解析
├── dynamic_columns.go    (新建) - 动态列管理
├── generator_factory.go  (新建) - 生成器工厂
├── config.go             (修改) - 添加配置字段
├── scheduler.go          (修改) - 集成动态列
└── worker.go             (修改) - 支持动态 Filler
```

## 当前状态总结

✅ **配置设计完成** - 70+ 字段的 YAML 配置已就绪  
❌ **代码实现待完成** - 需要实现配置解析和动态列支持  
✅ **文档完整** - 使用指南、方案设计都已完成  
✅ **临时方案可用** - Disk 模式可以立即使用  

---

**建议行动**:

1. **立即**: 使用 disk 模式进行测试（已配置好）
2. **短期**: 实现 Phase 1 的代码支持（2-3 小时）
3. **长期**: 完成 Phase 2 的完整功能（数组、条件等）

**联系方式**: 如需帮助实现代码部分，请告知具体需求。
