# DNS 日志数据生成使用指南

本指南说明如何使用配置文件方式为 `cdns_log` 表生成测试数据，无需修改任何代码。

## 📋 前提条件

1. ClickHouse 数据库已安装并运行
2. `dnsmon.cdns_log` 表已创建
3. ClickCannon 已编译完成

## 🚀 快速开始

### 步骤 1: 确认表结构

确保 ClickHouse 中存在 `dnsmon.cdns_log` 表，包含以下字段:

```sql
-- 查看表结构
DESC dnsmon.cdns_log;

-- 查看表数据量
SELECT count() FROM dnsmon.cdns_log;
```

### 步骤 2: 查看配置文件

项目已包含两个预配置文件:

- **`profiles/dns_logs_profile.yaml`** - DNS 日志字段生成规则
- **`config_dns_logs.yaml`** - 运行配置

查看并根据需要调整参数:

```bash
# 查看字段生成规则
cat profiles/dns_logs_profile.yaml

# 查看运行配置
cat config_dns_logs.yaml
```

### 步骤 3: 调整配置参数

编辑 `config_dns_logs.yaml`:

```yaml
generate:
  threads: 8                    # 根据 CPU 核心数调整
  rows_per_second: 50000        # 限速 (0=不限速)
  rows_per_block: 10000         # 每批次行数

insert:
  threads: 4                    # 插入线程数
  batch_size: 50000             # 批量插入大小
  clickhouse:
    address: "YOUR_CH_HOST:9000"  # 修改为你的地址
    user: "YOUR_USER"
    password: "YOUR_PASSWORD"
```

### 步骤 4: 运行生成器

```bash
# 运行 DNS 日志生成器
./clickcannon -config config_dns_logs.yaml
```

### 步骤 5: 监控数据生成

在另一个终端监控数据写入:

```bash
# 实时监控数据量
watch -n 1 "clickhouse-client -q \"SELECT count(), max(tnow) FROM dnsmon.cdns_log\""

# 查看最新数据
clickhouse-client -q "
SELECT 
    tnow,
    serverAddress,
    clientAddress,
    firstQueryName,
    responseRcode,
    responseDelay
FROM dnsmon.cdns_log
ORDER BY tnow DESC
LIMIT 10
"
```

## 📊 性能调优

### 提高生成速度

```yaml
generate:
  threads: 16              # 增加生成线程
  rows_per_second: 0       # 取消限速
  rows_per_block: 20000    # 增大 block 大小

insert:
  threads: 8               # 增加插入线程
  batch_size: 100000       # 增大批次大小
```

### 降低资源占用

```yaml
generate:
  threads: 2
  rows_per_second: 10000   # 限速 1万/秒
  rows_per_block: 5000

insert:
  threads: 2
  batch_size: 10000
```

## 🎯 自定义数据分布

### 修改域名列表

编辑 `profiles/dns_logs_profile.yaml`:

```yaml
custom_fields:
  firstQueryName:
    generator:
      type: pool
      values:
        - "your-domain-1.com"
        - "your-domain-2.com"
        - "your-domain-3.com"
      weights: [0.5, 0.3, 0.2]  # 50%, 30%, 20%
```

### 修改服务器地址池

```yaml
custom_fields:
  serverAddress:
    generator:
      type: pool
      values:
        - "10.160.152.186"
        - "10.160.152.187"
      weights: [0.7, 0.3]
```

### 调整恶意域名比例

```yaml
custom_fields:
  domainSecurityType:
    generator:
      type: weighted_pool
      values:
        - value: 0  # 正常
          weight: 70   # 降低到 70%
        - value: 3  # 恶意
          weight: 15   # 提高到 15%
```

### 增加响应延迟

```yaml
custom_fields:
  responseDelay:
    generator:
      type: int_range
      min: 10
      max: 5000  # 增加到 5 秒
```

## 📈 监控和指标

### 查看生成性能

```bash
# 查看 clickcannon 指标表
clickhouse-client -q "
SELECT 
    timestamp,
    rows_per_second,
    bytes_per_second,
    active_generators,
    active_inserters
FROM clickcannon.perf
WHERE run_id = (SELECT max(run_id) FROM clickcannon.runs)
ORDER BY timestamp DESC
LIMIT 20
"
```

### 分析数据分布

```sql
-- 查询类型分布
SELECT 
    firstType,
    count() as cnt,
    round(cnt * 100.0 / sum(cnt) OVER (), 2) as percentage
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY firstType
ORDER BY cnt DESC;

-- 响应码分布
SELECT 
    responseRcode,
    count() as cnt
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY responseRcode
ORDER BY cnt DESC;

-- TOP 域名
SELECT 
    firstQueryName,
    count() as cnt
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY firstQueryName
ORDER BY cnt DESC
LIMIT 20;

-- 安全威胁统计
SELECT 
    domainSecurityType,
    count() as cnt,
    CASE domainSecurityType
        WHEN 0 THEN '正常'
        WHEN 1 THEN '白域名'
        WHEN 2 THEN '未知'
        WHEN 3 THEN '恶意'
        WHEN 4 THEN '内容拦截'
        WHEN 5 THEN '自定义拦截'
    END as type_name
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY domainSecurityType
ORDER BY cnt DESC;

-- 响应延迟分析
SELECT 
    quantile(0.50)(responseDelay) as p50,
    quantile(0.95)(responseDelay) as p95,
    quantile(0.99)(responseDelay) as p99,
    max(responseDelay) as max
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR;
```

## ⚠️ 注意事项

### 1. 数组字段暂不支持

以下字段在当前配置中使用空值占位:

- `otherQueries.*`
- `responseAnswerRrs.*`
- `responseAuthorityRrs.*`
- `responseAdditionalRrs.*`
- `xForwardIp`
- `contentTags`

这些字段需要在代码中特殊处理，或等待 Phase 2 实现数组类型支持。

### 2. MATERIALIZED 字段

`domain` 字段是 `MATERIALIZED`，由 ClickHouse 自动从 `firstQueryName` 计算，不需要在配置中定义。

### 3. 性能建议

- **生成线程数** 建议设置为 CPU 核心数的 1-2 倍
- **插入线程数** 建议设置为 2-8 个
- **批次大小** 建议 10,000 - 100,000 行
- 启用 **compression** (lz4 或 zstd) 可减少网络传输

### 4. 数据一致性

- `dnsMessageId` 在 Query/Response 配对中应该相同
- `clientPort` 和 `serverPort` 的组合应该合理
- 当前版本是随机生成，不保证严格的 DNS 协议逻辑

## 🔧 故障排查

### 无法连接 ClickHouse

```bash
# 测试连接
clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "SELECT version()"
```

### 插入速度慢

1. 检查 ClickHouse 负载: `SELECT * FROM system.processes`
2. 增加 `insert.threads` 和 `generate.threads`
3. 启用压缩: `compression: "lz4"`
4. 增大批次: `batch_size: 100000`

### 内存占用高

1. 减少线程数
2. 减小 `rows_per_block` 和 `batch_size`
3. 启用 `reuse_blocks: true`
4. 设置 `block_retirement_uses: 50`

### 数据分布不符合预期

1. 检查 `dns_logs_profile.yaml` 中的权重设置
2. 调整 `weights` 数组的值
3. 修改 `values` 池中的选项

## 📚 相关文档

- [数据生成流程与自定义配置指南](./数据生成流程与自定义配置指南.md)
- [配置文件说明](./config.md)
- [性能优化指南](./MONITORING.md)

## 🎓 示例场景

### 场景 1: 压力测试 - 每秒 10 万行

```yaml
generate:
  threads: 16
  rows_per_second: 100000
  rows_per_block: 20000

insert:
  threads: 8
  batch_size: 50000
```

### 场景 2: 长期稳定测试 - 每秒 1 万行

```yaml
generate:
  threads: 4
  rows_per_second: 10000
  rows_per_block: 10000

insert:
  threads: 2
  batch_size: 20000
```

### 场景 3: 安全测试 - 高恶意域名比例

```yaml
# 在 dns_logs_profile.yaml 中修改:
domainSecurityType:
  generator:
    values:
      - value: 0
        weight: 50  # 正常 50%
      - value: 3
        weight: 30  # 恶意 30%
      - value: 4
        weight: 20  # 内容拦截 20%
```

## ✅ 验证数据正确性

```sql
-- 检查字段覆盖率
SELECT 
    count() as total,
    countIf(serverAddress != '') as has_server,
    countIf(clientAddress != '') as has_client,
    countIf(firstQueryName != '') as has_query,
    countIf(responseDelay > 0) as has_delay
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 10 MINUTE;

-- 检查数据类型
SELECT 
    toTypeName(tnow) as tnow_type,
    toTypeName(serverPort) as port_type,
    toTypeName(responseDelay) as delay_type
FROM dnsmon.cdns_log
LIMIT 1;

-- 检查数值范围
SELECT 
    min(serverPort) as min_port,
    max(serverPort) as max_port,
    min(responseDelay) as min_delay,
    max(responseDelay) as max_delay
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR;
```

---

**版本**: v1.0  
**最后更新**: 2026-08-10
