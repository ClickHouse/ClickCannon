# 🚀 立即运行指南

## ✅ 准备工作已完成

1. ✅ 代码已实现配置化支持
2. ✅ 代码已编译成功
3. ✅ 配置文件已验证（70+ 字段）
4. ✅ Generate 模式已启用

## 🎯 立即运行

### 方式 1: 直接运行（推荐测试）

```bash
# 运行 10 秒测试
timeout 10 ./clickcannon -config config_dns_logs.yaml

# 查看是否有数据
clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "SELECT count() FROM dnsmon.cdns_log WHERE tnow >= now() - INTERVAL 1 MINUTE"
```

### 方式 2: 后台运行

```bash
# 后台运行
nohup ./clickcannon -config config_dns_logs.yaml > clickcannon.log 2>&1 &

# 查看日志
tail -f clickcannon.log

# 停止
pkill clickcannon
```

### 方式 3: 使用测试脚本

```bash
./test_generate.sh
```

## 📊 监控数据生成

### 实时监控行数

```bash
watch -n 1 "clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query 'SELECT count() FROM dnsmon.cdns_log'"
```

### 查看最新数据

```bash
clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "
SELECT 
    tnow,
    serverAddress,
    clientAddress,
    firstQueryName,
    firstType,
    responseRcode,
    responseDelay
FROM dnsmon.cdns_log
ORDER BY tnow DESC
LIMIT 10
FORMAT Vertical
"
```

### 查看数据分布

```bash
clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "
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
WHERE tnow >= now() - INTERVAL 10 MINUTE
GROUP BY domainSecurityType
ORDER BY cnt DESC
"
```

## ⚙️ 调整性能

### 提高速度

编辑 `config_dns_logs.yaml`:

```yaml
generate:
  threads: 16              # 增加到 16 线程
  rows_per_second: 0       # 取消限速（全速）
  rows_per_block: 20000    # 增大 block
```

### 降低速度

```yaml
generate:
  threads: 2
  rows_per_second: 10000   # 限速 1万/秒
  rows_per_block: 5000
```

## 🎨 调整数据分布

编辑 `profiles/dns_logs_profile.yaml`:

### 增加恶意域名比例

```yaml
domainSecurityType:
  generator:
    values:
      - value: 0   # 正常
        weight: 50  # 改为 50%
      - value: 3   # 恶意
        weight: 30  # 改为 30%
```

### 修改域名列表

```yaml
firstQueryName:
  generator:
    values:
      - "www.example.com"
      - "api.example.com"
      - "你自己的域名.com"
```

### 修改服务器地址

```yaml
serverAddress:
  generator:
    values:
      - "10.160.152.186"
      - "10.160.152.187"
      - "你的服务器IP"
```

修改后**无需重新编译**，直接重启即可生效。

## 🔍 故障排查

### 检查配置

```bash
./check_config.sh
```

### 查看日志

```bash
./clickcannon -config config_dns_logs.yaml 2>&1 | tee run.log
```

检查日志中是否有:
- ✓ "loaded custom fields config"
- ✓ "fields_count=70" (或更多)
- ✓ "started" (workers 启动)

### 常见问题

**问题 1**: "panic: runtime error"
- 原因: 配置文件格式错误
- 解决: 运行 `./check_config.sh` 检查

**问题 2**: "failed to load custom fields config"  
- 原因: 文件路径不对
- 解决: 确认 `profiles/dns_logs_profile.yaml` 存在

**问题 3**: "NO_SUCH_COLUMN_IN_TABLE"
- 原因: 表结构不匹配
- 解决: 确认表名为 `dnsmon.cdns_log`

**问题 4**: 无数据写入
- 检查 ClickHouse 连接
- 检查表是否存在
- 查看 clickcannon 日志

## 📈 性能指标

预期性能（8核CPU）:
- **线程数: 8** → 约 5-8 万行/秒
- **线程数: 16** → 约 10-15 万行/秒
- **内存占用**: 2-4 GB
- **CPU 使用**: 30-60%

## ✅ 成功标志

运行成功后，你应该看到:

```
time=... level=INFO msg="loaded custom fields config" file=profiles/dns_logs_profile.yaml fields_count=70
time=... level=INFO msg="using custom fields generator"
time=... level=INFO msg=started component=generate_scheduler
time=... level=INFO msg=started component=generate_scheduler worker_id=0
time=... level=INFO msg=started component=generate_scheduler worker_id=1
...
```

ClickHouse 中应该有数据:

```sql
SELECT count() FROM dnsmon.cdns_log;
-- 应该返回 > 0
```

## 🎉 下一步

1. **运行测试**: `./test_generate.sh`
2. **调整配置**: 根据需求修改 `profiles/dns_logs_profile.yaml`
3. **长期运行**: 使用 nohup 或 systemd 服务
4. **监控指标**: 查看 ClickHouse 中的数据

---

**准备好了！现在运行:**

```bash
./clickcannon -config config_dns_logs.yaml
```

或使用测试脚本:

```bash
./test_generate.sh
```
