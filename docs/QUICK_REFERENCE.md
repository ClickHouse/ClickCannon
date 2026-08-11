# ClickCannon 快速参考指南

## 🚀 快速开始

### 编译
```bash
go build -o clickcannon
```

### 运行
```bash
./clickcannon -config config_dns_logs.yaml
```

### 停止
```bash
Ctrl+C  或  kill <pid>
```

## 📝 配置文件

### 主配置: `config_dns_logs.yaml`
```yaml
generate:
  enabled: true
  enable_custom_fields: true
  profile_config_file: "profiles/dns_logs_profile.yaml"
  rows_per_second: 50000
  threads: 8

insert:
  enabled: true
  threads: 4
  batch_size: 50000
```

### 字段配置: `profiles/dns_logs_profile.yaml`

## 🔧 常用修改

### 1. 修改整数范围
```yaml
clientPort:
  generator:
    type: int_range
    min: 1024      # 最小值
    max: 65535     # 最大值
```

### 2. 修改字符串池
```yaml
firstQueryName:
  generator:
    type: pool
    values:
      - "domain1.com"
      - "domain2.com"
    weights: [0.7, 0.3]  # 权重（可选）
```

### 3. 修改数组大小
```yaml
responseAnswerRrs.name:
  array_config:
    min_size: 0    # 最小元素数
    max_size: 3    # 最大元素数
```

### 4. 修改时间戳格式
```yaml
tnow:
  generator:
    type: timestamp
    format: unix_sec    # unix_sec / unix_milli / unix_nano
    offset_sec: 0       # 时间偏移（秒）
```

## 📊 生成器类型速查

| 类型 | 用途 | 必需参数 |
|------|------|----------|
| `const` | 常量 | `value` |
| `pool` | 随机池 | `values` |
| `weighted_pool` | 加权池 | `values` + `weights` |
| `uuid` | UUID | 无 |
| `random_string` | 随机字符串 | `length`, `charset` |
| `hex` | 十六进制 | `length` |
| `int_range` | 整数范围 | `min`, `max` |
| `float_range` | 浮点范围 | `min`, `max` |
| `ip_v4` | IP地址 | `format: dotted` |
| `timestamp` | 时间戳 | `format` |

## 🐛 故障排查

### 问题：字段值都是0
**原因**: 配置文件中 min/max 未正确设置  
**解决**: 检查 yaml 格式，确保 min 和 max 在正确的缩进层级

### 问题：数组大小不匹配
**原因**: Nested 结构的多个数组配置了不同的 min/max  
**解决**: 确保同一 Nested 组的所有字段使用相同的 min_size 和 max_size

### 问题：NO_SUCH_COLUMN
**原因**: 配置文件中的字段名与数据库表不匹配  
**解决**: 检查字段名，数组字段使用点分隔（如 `otherQueries.name`）

### 问题：编译错误
**原因**: Go 版本过低或依赖缺失  
**解决**: 
```bash
go version  # 需要 Go 1.18+
go mod tidy
go build
```

## 📈 性能调优

### 提高生成速率
```yaml
generate:
  rows_per_second: 100000  # 从 50000 提高到 100000
  threads: 16              # 增加并发
```

### 提高写入速度
```yaml
insert:
  threads: 8               # 增加插入线程
  batch_size: 100000       # 增大批次
```

### 降低资源占用
```yaml
generate:
  rows_per_second: 10000   # 降低速率
  threads: 4               # 减少并发
```

## 🔍 数据验证

### 查看生成的数据
```sql
SELECT * FROM dnsmon.cdns_log 
WHERE tnow >= now() - INTERVAL 5 MINUTE 
LIMIT 10;
```

### 统计信息
```sql
SELECT 
    count() as total,
    min(tnow) as first_time,
    max(tnow) as last_time,
    countDistinct(serverAddress) as unique_servers,
    avg(responseDelay) as avg_delay
FROM dnsmon.cdns_log 
WHERE tnow >= now() - INTERVAL 1 HOUR;
```

### 数组字段验证
```sql
SELECT 
    firstQueryName,
    length(responseAnswerRrs.name) as answer_count,
    length(otherQueries.name) as other_count,
    responseAnswerRrs.name,
    responseAnswerRrs.type
FROM dnsmon.cdns_log 
WHERE tnow >= now() - INTERVAL 5 MINUTE
LIMIT 10;
```

## 💡 最佳实践

### 1. 字段权重设计
为常见值设置更高的权重：
```yaml
transport:
  values: [1, 2, 3, 5]      # UDP, TCP, TLS, HTTPS
  weights: [75, 20, 3, 2]   # UDP 占比75%
```

### 2. 数组大小设计
大多数情况使用小数组，偶尔大数组：
```yaml
array_config:
  min_size: 0    # 允许空数组
  max_size: 3    # 大多数情况1-3个元素
```

### 3. 时间戳对齐
使用当前时间便于查询最新数据：
```yaml
tnow:
  generator:
    type: timestamp
    format: unix_sec
    offset_sec: 0    # 0=当前时间
```

### 4. IP 地址范围
使用内网IP便于识别测试数据：
```yaml
clientAddress:
  generator:
    type: ip_v4
    format: dotted
```

## 📦 配置模板

### 基础字段模板
```yaml
  field_name:
    type: string
    clickhouse_type: String
    generator:
      type: const
      value: "default"
```

### 数组字段模板
```yaml
  array_field:
    type: array_string
    clickhouse_type: Array(String)
    is_array: true
    array_config:
      min_size: 0
      max_size: 5
      elements:
        type: pool
        values: ["value1", "value2"]
```

### Nested 数组组模板
```yaml
  nested.field1:
    type: array_string
    is_array: true
    array_config:
      min_size: 0
      max_size: 3
      elements: {type: pool, values: ["a", "b"]}
  
  nested.field2:
    type: array_uint16
    is_array: true
    array_config:
      min_size: 0
      max_size: 3      # 必须与 field1 相同
      elements: {type: const, value: 1}
```

## 📞 获取帮助

- 查看详细文档: `FINAL_REPORT.md`
- 技术实现: `IMPLEMENTATION_SUMMARY.md`
- 配置示例: `profiles/dns_logs_profile.yaml`
- 源代码: `internal/generate/*.go`
