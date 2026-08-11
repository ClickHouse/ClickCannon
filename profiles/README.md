# ClickCannon 自定义字段配置目录

此目录包含通过配置文件自定义数据生成字段的示例和文档。

## 📁 文件说明

### 配置文件

- **`quickstart.yaml`** - 快速入门配置，包含 3 个简单的自定义字段
  - 适合: 第一次使用，快速测试
  - 字段: user_id, request_count, environment

- **`custom_logs_example.yaml`** - 完整的配置示例，展示所有支持的生成器类型
  - 适合: 学习所有可用功能
  - 包含: 12 种不同类型的字段示例 + 注释说明

### SQL 文件

- **`quickstart.sql`** - 对应 quickstart.yaml 的建表语句
- **`clickhouse_table_example.sql`** - 对应 custom_logs_example.yaml 的完整建表 + 查询示例

## 🚀 快速开始

### 1. 使用 quickstart 配置

```bash
# 1. 创建 ClickHouse 表
clickhouse-client < profiles/quickstart.sql

# 2. 修改 config.yaml
cat >> config.yaml << EOF
generate:
  enabled: true
  enable_custom_fields: true
  profile_config_file: "profiles/quickstart.yaml"
  
insert:
  enabled: true
  clickhouse:
    logs_table: "quickstart_logs"
EOF

# 3. 运行
./clickcannon -config config.yaml
```

### 2. 自定义你的配置

```bash
# 复制示例配置
cp profiles/custom_logs_example.yaml profiles/my_config.yaml

# 编辑配置文件，添加你需要的字段
vim profiles/my_config.yaml

# 创建对应的 ClickHouse 表
# (根据你的字段修改 clickhouse_table_example.sql)

# 更新 config.yaml 指向你的配置
# profile_config_file: "profiles/my_config.yaml"
```

## 📖 支持的生成器类型

| 类型 | 用途 | 示例 |
|------|------|------|
| `const` | 固定值 | `type: const, value: "fixed"` |
| `pool` | 从列表随机选择 | `type: pool, values: ["a", "b"], weights: [0.7, 0.3]` |
| `weighted_pool` | 带权重选择 | `type: weighted_pool, values: [{value: 200, weight: 80}]` |
| `uuid` | UUID | `type: uuid` |
| `random_string` | 随机字符串 | `type: random_string, length: 16, charset: hex` |
| `hex` | 十六进制 | `type: hex, length: 32` |
| `int_range` | 整数范围 | `type: int_range, min: 1, max: 1000` |
| `float_range` | 浮点数范围 | `type: float_range, min: 0.0, max: 100.0` |
| `ip_v4` | IPv4 地址 | `type: ip_v4, format: dotted` |
| `timestamp` | 时间戳 | `type: timestamp, format: unix_milli` |

---

## 🎯 DNS 日志专用配置

项目已包含完整的 DNS 日志表 (`cdns_log`) 配置:

### 文件清单

- **`dns_logs_profile.yaml`** - DNS 日志字段生成规则 (70+ 字段)
- **`config_dns_logs.yaml`** - DNS 日志运行配置
- **`docs/DNS日志生成使用指南.md`** - 完整使用文档

### 快速使用

```bash
# 1. 确保 ClickHouse 表已创建 (dnsmon.cdns_log)

# 2. 运行生成器
./clickcannon -config config_dns_logs.yaml

# 3. 监控数据
watch -n 1 "clickhouse-client -q 'SELECT count() FROM dnsmon.cdns_log'"
```

### 包含的字段类型

- ✅ 基础网络字段 (IP, Port, Protocol)
- ✅ DNS 协议字段 (MessageId, QueryName, Type, Class)
- ✅ 安全字段 (SecurityType, Severity, Action)
- ✅ 性能字段 (Delay, Size)
- ✅ EDNS 字段
- ✅ DNSSEC 字段
- ✅ 威胁情报字段 (IOC, Family, Group)
- ⚠️ 数组字段 (暂不支持，等待 Phase 2)

详细说明见: [DNS日志生成使用指南](../docs/DNS日志生成使用指南.md)
