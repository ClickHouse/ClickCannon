# DNS 日志生成 - 快速开始

## ⚠️ 重要提示

**当前状态**: Generate 模式的配置化支持代码尚未实现（需要 Phase 1 开发）

**可用方案**:
1. ✅ **Disk 模式** - 使用现有数据文件重放（已配置，立即可用）
2. ⏳ **Generate 模式** - 需要实现代码支持（见 GENERATE_MODE_FIX.md）

## ⚡ 3 步开始 (Disk 模式)

```bash
# 1. 确认数据文件存在
ls log_data/*.native*

# 2. 运行生成器 (使用 disk 模式)
./clickcannon -config config_dns_logs.yaml

# 3. 查看数据
clickhouse-client -q "
SELECT count(), max(tnow) 
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 MINUTE
"
```

## 📝 关键配置

### 当前使用 Disk 模式

`config_dns_logs.yaml` 已配置为 disk 模式（因为 generate 模式需要代码实现）：

```yaml
disk:
  enabled: true
  threads: 16
  logs_path: log_data        # 你的数据目录
  loop: true                 # 循环播放
  shift_timestamp: now       # 使用当前时间
```

### Generate 模式 (待实现)

Generate 模式的配置化支持需要实现代码，详见 `GENERATE_MODE_FIX.md`

### 修改重放速度

编辑 `config_dns_logs.yaml`:

```yaml
disk:
  mb_per_second_limit: 2048  # 改为你想要的速度 (MB/s)
```

### 修改域名列表 (需要 Generate 模式)

**注意**: 当前使用 disk 模式重放现有数据，域名列表来自数据文件。

要使用配置文件定义域名，需要实现 Generate 模式的代码支持。

### 修改时间戳策略

编辑 `config_dns_logs.yaml`:

```yaml
disk:
  shift_timestamp: now     # 使用当前时间
  # 或
  shift_timestamp: date    # 保持时间，只改日期
  # 或
  shift_timestamp: all     # 保持原始时间间隔
```

## 📊 监控命令

```bash
# 实时监控数据量
watch -n 1 "clickhouse-client -q 'SELECT count() FROM dnsmon.cdns_log'"

# 查看最新 10 条
clickhouse-client -q "
SELECT tnow, serverAddress, clientAddress, firstQueryName 
FROM dnsmon.cdns_log 
ORDER BY tnow DESC 
LIMIT 10
"

# 查看数据分布
clickhouse-client -q "
SELECT 
    domainSecurityType,
    count() as cnt
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY domainSecurityType
"
```

## 🎯 常见场景

### 压力测试 (全速重放)

```yaml
disk:
  threads: 32
  mb_per_second_limit: 0   # 不限速
```

### 稳定运行 (限速 2 GB/s)

```yaml
disk:
  threads: 16
  mb_per_second_limit: 2048
```

### 循环播放

```yaml
disk:
  loop: true               # 数据播放完后重新开始
```

## 🔄 Generate 模式实现

如需使用配置文件生成数据（而非重放现有数据），请参考：

- **实现指南**: [GENERATE_MODE_FIX.md](GENERATE_MODE_FIX.md)
- **预计开发时间**: 2-3 小时（Phase 1 基础支持）

实现后可以使用 `dns_logs_profile.yaml` 配置文件定义数据生成规则。

## 🔧 故障排查

### 无法连接

```bash
# 测试连接
clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "SELECT 1"
```

### 速度太慢

1. 增加 `generate.threads`
2. 增加 `insert.threads`
3. 增大 `batch_size`
4. 启用压缩: `compression: "lz4"`

### 内存占用高

1. 减少 `threads`
2. 减小 `rows_per_block`
3. 减小 `batch_size`

## 📚 详细文档

- **完整指南**: [docs/DNS日志生成使用指南.md](docs/DNS日志生成使用指南.md)
- **配置说明**: [docs/数据生成流程与自定义配置指南.md](docs/数据生成流程与自定义配置指南.md)
- **方案总结**: [DNS_LOGS_SUMMARY.md](DNS_LOGS_SUMMARY.md)

## ✅ 验证数据

```sql
-- 检查字段完整性
SELECT 
    count() as total,
    countIf(serverAddress != '') as has_server,
    countIf(clientAddress != '') as has_client,
    countIf(firstQueryName != '') as has_query,
    avg(responseDelay) as avg_delay
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 10 MINUTE;

-- 检查数据分布
SELECT 
    firstType,
    count() as cnt
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY firstType
ORDER BY cnt DESC;
```

---

**需要帮助?** 查看 [DNS日志生成使用指南](docs/DNS日志生成使用指南.md)
