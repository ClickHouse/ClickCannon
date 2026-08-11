# 数组字段临时解决方案

## 问题说明

当前配置化方案 (Phase 1) 暂不支持数组类型字段，以下字段无法通过配置文件生成:

### cdns_log 表中的数组字段

1. **otherQueries**
   - `otherQueries.name` Array(String)
   - `otherQueries.class` Array(LowCardinality(UInt16))
   - `otherQueries.type` Array(LowCardinality(UInt16))

2. **responseAnswerRrs**
   - `responseAnswerRrs.name` Array(String)
   - `responseAnswerRrs.class` Array(LowCardinality(UInt16))
   - `responseAnswerRrs.type` Array(LowCardinality(UInt16))
   - `responseAnswerRrs.ttl` Array(LowCardinality(UInt32))
   - `responseAnswerRrs.rdata` Array(String)

3. **responseAuthorityRrs**
   - (同上结构)

4. **responseAdditionalRrs**
   - (同上结构)

5. **其他数组字段**
   - `xForwardIp` Array(String)
   - `contentTags` Array(String)

## 临时解决方案

### 方案 1: 使用空数组 (推荐)

在 ClickHouse 表中设置默认值为空数组:

```sql
ALTER TABLE dnsmon.cdns_log
MODIFY COLUMN `otherQueries.name` Array(String) DEFAULT [],
MODIFY COLUMN `responseAnswerRrs.name` Array(String) DEFAULT [],
MODIFY COLUMN `xForwardIp` Array(String) DEFAULT [];
```

插入时这些字段会自动使用空数组，不影响其他字段的数据生成。

### 方案 2: 修改表结构 (临时)

如果这些字段不是测试重点，可以临时改为 Nullable:

```sql
-- 备份表
CREATE TABLE dnsmon.cdns_log_backup AS dnsmon.cdns_log;

-- 删除原表
DROP TABLE dnsmon.cdns_log;

-- 重建表 (移除数组字段)
CREATE TABLE dnsmon.cdns_log
(
    tnow DateTime,
    serverAddress LowCardinality(String),
    -- ... 其他非数组字段 ...
    
    -- 注释掉数组字段
    -- `otherQueries.name` Array(String),
    -- `responseAnswerRrs.name` Array(String),
    -- ...
)
ENGINE = MergeTree()
ORDER BY (tnow);
```

### 方案 3: 代码方式生成数组字段

如果必须包含数组字段，需要创建自定义 Profile:

```go
// internal/generate/profile_dns_logs.go
package generate

func init() { RegisterProfile("dns_logs", newProfileDnsLogs) }

func newProfileDnsLogs() *Profile {
    return NewProfile("dns_logs").
        // ... 其他字段配置 ...
        WithCustomArrayField("otherQueries.name", func(rng *Rng) []string {
            // 50% 概率返回空数组
            if rng.IntN(2) == 0 {
                return []string{}
            }
            // 否则返回 1-3 个额外查询名称
            count := rng.IntN(3) + 1
            result := make([]string, count)
            for i := 0; i < count; i++ {
                result[i] = fmt.Sprintf("extra%d.example.com", i)
            }
            return result
        })
}
```

然后修改 Filler 逻辑来填充这些数组。

### 方案 4: 使用 Nested 类型的字符串表示

将数组转换为 JSON 字符串存储:

```sql
-- 修改为字符串类型
ALTER TABLE dnsmon.cdns_log
MODIFY COLUMN `otherQueries.name` String DEFAULT '[]';

-- 查询时解析
SELECT 
    JSONExtract(`otherQueries.name`, 'Array(String)') as other_queries
FROM dnsmon.cdns_log;
```

在配置文件中:

```yaml
custom_fields:
  otherQueries_name:
    type: string
    generator:
      type: pool
      values:
        - '[]'
        - '["extra1.example.com"]'
        - '["extra1.example.com", "extra2.example.com"]'
      weights: [0.7, 0.2, 0.1]
```

## 推荐策略

### 对于压力测试

如果主要目的是测试 ClickHouse 的写入性能和查询性能:

1. ✅ 使用**方案 1** (空数组默认值)
2. ✅ 确保非数组字段的数据完整性
3. ✅ 重点测试核心查询场景

**优点**: 
- 无需修改代码
- 配置文件即可完成
- 性能测试不受影响

### 对于功能测试

如果需要测试数组字段的查询逻辑:

1. ✅ 使用**方案 3** (代码生成)
2. ✅ 创建自定义 Profile
3. ✅ 实现合理的数组数据生成逻辑

**优点**:
- 数据更真实
- 可以测试数组查询
- 可控制数组大小和分布

### 对于生产环境模拟

如果需要模拟真实的 DNS 响应:

1. ✅ 使用 **disk 模式** 替代 generate 模式
2. ✅ 导出真实的 DNS 日志
3. ✅ 使用 disk 模式重放

**优点**:
- 数据完全真实
- 包含所有数组字段
- 反映真实业务特征

## Phase 2 计划

数组类型支持将在 Phase 2 实现，届时配置文件将支持:

```yaml
custom_fields:
  otherQueries_name:
    type: array
    element_type: string
    generator:
      type: array
      min_length: 0
      max_length: 3
      element_generator:
        type: pool
        values: ["extra1.com", "extra2.com", "extra3.com"]
  
  responseAnswerRrs_ttl:
    type: array
    element_type: uint32
    generator:
      type: array
      min_length: 1
      max_length: 5
      element_generator:
        type: int_range
        min: 300
        max: 86400
```

## 当前建议

对于 DNS 日志压测场景:

```yaml
# 使用 dns_logs_profile.yaml 配置
# 数组字段会使用表的默认值 (空数组)

generate:
  enabled: true
  enable_custom_fields: true
  profile_config_file: "profiles/dns_logs_profile.yaml"
```

表结构保持不变，数组字段使用默认值:

```sql
-- 确认默认值
SHOW CREATE TABLE dnsmon.cdns_log;

-- 如果没有默认值，添加:
ALTER TABLE dnsmon.cdns_log
MODIFY COLUMN `otherQueries.name` Array(String) DEFAULT [],
MODIFY COLUMN `otherQueries.class` Array(LowCardinality(UInt16)) DEFAULT [],
MODIFY COLUMN `otherQueries.type` Array(LowCardinality(UInt16)) DEFAULT [],
MODIFY COLUMN `responseAnswerRrs.name` Array(String) DEFAULT [],
-- ... 其他数组字段 ...
```

## 验证方案

```sql
-- 插入测试数据后检查
SELECT 
    count() as total,
    countIf(length(`otherQueries.name`) = 0) as empty_arrays,
    countIf(serverAddress != '') as has_server,
    countIf(firstQueryName != '') as has_query
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 MINUTE;
```

预期结果:
- `total` > 0 (有数据写入)
- `empty_arrays` = `total` (数组字段为空)
- `has_server` = `total` (非数组字段有数据)
- `has_query` = `total` (非数组字段有数据)

---

**状态**: 临时方案  
**等待**: Phase 2 实现数组类型支持  
**更新时间**: 2026-08-10
