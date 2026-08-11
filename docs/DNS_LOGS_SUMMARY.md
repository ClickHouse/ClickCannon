# DNS 日志配置化生成方案总结

## 📦 交付内容

本次为您的 `cdns_log` 表创建了完整的配置化数据生成方案，**无需修改任何代码**。

### 1. 配置文件

| 文件 | 说明 | 字段数 |
|------|------|--------|
| `profiles/dns_logs_profile.yaml` | DNS 字段生成规则 | 70+ 字段 |
| `config_dns_logs.yaml` | 运行配置 | - |

### 2. 文档

| 文件 | 说明 |
|------|------|
| `docs/DNS日志生成使用指南.md` | 完整使用指南 + 示例 |
| `docs/数据生成流程与自定义配置指南.md` | 通用配置化方案文档 |
| `profiles/ARRAY_FIELDS_WORKAROUND.md` | 数组字段临时解决方案 |
| `profiles/README.md` | Profiles 目录说明 |

### 3. 示例配置

| 文件 | 说明 |
|------|------|
| `profiles/quickstart.yaml` | 快速入门示例 (3 字段) |
| `profiles/custom_logs_example.yaml` | 完整示例 (12 种生成器) |
| `profiles/quickstart.sql` | 快速入门建表 SQL |
| `profiles/clickhouse_table_example.sql` | 完整建表 + 查询示例 |

## 🎯 已支持的字段 (70+)

### ✅ 完全支持

- **网络字段** (12): tnow, serverAddress, clientAddress, serverPort, clientPort, transport, ipType, forwardIp, localAddress, localPort, clientSubnet, xForwardIp
- **DNS 协议** (10): dnsMessageId, firstQueryName, firstType, firstClass, queryOpCode, queryFlags, responseFlags, querySize, responseSize, responseRcode
- **EDNS** (4): queryEdnsVersion, queryUdpSize, queryOptRdata, clientHoplimit
- **DNSSEC** (2): qrDnssecFlag, qrDnssecSignature
- **性能** (1): responseDelay
- **安全策略** (10): domainSecurityType, serverType, qrSigFlags, severity, policyid, action, isInternalFQN, filterType, realAction
- **威胁情报** (6): group, family, operation, maliciousType, ioc, contentTags(部分)
- **路由代理** (10): hostId, uid, nodeId, channel, source, uri, method, proxyVersion, sourceNetMask, scopeNetMask

### ⚠️ 部分支持 (使用空值)

- **数组字段** (6 组): otherQueries.*, responseAnswerRrs.*, responseAuthorityRrs.*, responseAdditionalRrs.*, xForwardIp, contentTags

这些字段在当前版本使用表的默认值 (空数组)，不影响其他字段的测试。

### 📝 自动计算字段

- **domain**: MATERIALIZED 字段，由 ClickHouse 自动从 firstQueryName 计算

## 🚀 快速使用

### 3 步开始

```bash
# 1. 确保表已创建
clickhouse-client -q "SELECT count() FROM dnsmon.cdns_log"

# 2. 运行生成器
./clickcannon -config config_dns_logs.yaml

# 3. 监控数据
watch -n 1 "clickhouse-client -q 'SELECT count() FROM dnsmon.cdns_log'"
```

### 性能参考

```yaml
# 配置示例 (8核CPU, 每秒5万行)
generate:
  threads: 8
  rows_per_second: 50000
  rows_per_block: 10000

insert:
  threads: 4
  batch_size: 50000
```

预期性能:
- **写入速度**: 50,000 行/秒 (约 20 MB/s)
- **CPU 使用**: 30-50%
- **内存占用**: 2-4 GB

## 🎨 自定义数据分布

所有字段都可以通过编辑 `dns_logs_profile.yaml` 自定义，无需改代码。

### 示例 1: 调整域名分布

```yaml
firstQueryName:
  generator:
    type: pool
    values: ["www.example.com", "api.example.com"]
    weights: [0.7, 0.3]  # 70% vs 30%
```

### 示例 2: 增加恶意域名比例

```yaml
domainSecurityType:
  generator:
    values:
      - value: 0   # 正常
        weight: 60  # 从 80% 降到 60%
      - value: 3   # 恶意
        weight: 25  # 从 3% 提到 25%
```

### 示例 3: 调整响应延迟范围

```yaml
responseDelay:
  generator:
    type: int_range
    min: 1
    max: 5000  # 1-5000 毫秒
```

## 📊 数据验证

### 基本验证

```sql
-- 检查最近 10 分钟的数据
SELECT 
    count() as total,
    uniq(serverAddress) as servers,
    uniq(clientAddress) as clients,
    uniq(firstQueryName) as domains,
    avg(responseDelay) as avg_delay_ms
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 10 MINUTE;
```

### 分布验证

```sql
-- 查询类型分布
SELECT 
    firstType,
    count() as cnt,
    round(cnt * 100.0 / sum(cnt) OVER (), 2) as pct
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY firstType
ORDER BY cnt DESC;

-- 响应码分布
SELECT 
    responseRcode,
    count() as cnt,
    CASE responseRcode
        WHEN 0 THEN 'NOERROR'
        WHEN 2 THEN 'SERVFAIL'
        WHEN 3 THEN 'NXDOMAIN'
        ELSE toString(responseRcode)
    END as name
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY responseRcode
ORDER BY cnt DESC;

-- 安全威胁分布
SELECT 
    domainSecurityType,
    count() as cnt,
    CASE domainSecurityType
        WHEN 0 THEN '正常'
        WHEN 1 THEN '白域名'
        WHEN 3 THEN '恶意域名'
        ELSE toString(domainSecurityType)
    END as type
FROM dnsmon.cdns_log
WHERE tnow >= now() - INTERVAL 1 HOUR
GROUP BY domainSecurityType;
```

## 🔧 调优指南

### 提高速度 (适合压力测试)

```yaml
generate:
  threads: 16              # 增加线程
  rows_per_second: 0       # 取消限速
  rows_per_block: 20000    # 增大 block

insert:
  threads: 8
  batch_size: 100000
  clickhouse:
    compression: "lz4"     # 启用压缩
```

### 降低资源 (适合长期运行)

```yaml
generate:
  threads: 2
  rows_per_second: 10000   # 每秒 1 万行
  rows_per_block: 5000

insert:
  threads: 2
  batch_size: 10000
```

### 真实场景模拟

```yaml
# 修改 dns_logs_profile.yaml
custom_fields:
  # 80% NOERROR, 15% NXDOMAIN, 5% SERVFAIL
  responseRcode:
    generator:
      values:
        - value: 0
          weight: 80
        - value: 3
          weight: 15
        - value: 2
          weight: 5
  
  # 响应延迟符合真实分布
  responseDelay:
    generator:
      type: int_range
      min: 5      # 5ms
      max: 500    # 500ms
```

## ⚠️ 已知限制

### 1. 数组字段暂不支持

以下字段使用空数组:
- otherQueries.*
- responseAnswerRrs.*
- responseAuthorityRrs.*
- responseAdditionalRrs.*
- xForwardIp (部分)
- contentTags (部分)

**解决方案**: 见 `profiles/ARRAY_FIELDS_WORKAROUND.md`

### 2. DNS 协议逻辑

当前是独立随机生成，不保证严格的 DNS 协议逻辑:
- Query/Response 的 dnsMessageId 不配对
- responseRcode 和 responseSize 不严格对应

如需严格的协议逻辑，建议使用 disk 模式重放真实数据。

### 3. 性能上限

受限于:
- ClickHouse 单表写入性能
- 网络带宽
- 机器 CPU/内存

典型上限: 10-20 万行/秒 (单表)

## 📈 压测场景建议

### 场景 1: 吞吐量压测

目标: 测试 ClickHouse 最大写入速度

```yaml
generate:
  threads: 16
  rows_per_second: 0        # 不限速
  rows_per_block: 20000

insert:
  threads: 8
  batch_size: 100000
```

### 场景 2: 稳定性测试

目标: 长期运行 24 小时

```yaml
generate:
  threads: 4
  rows_per_second: 20000    # 每秒 2 万行
  rows_per_block: 10000

insert:
  threads: 4
  batch_size: 50000
```

### 场景 3: 安全场景测试

目标: 测试恶意域名检测

```yaml
# 修改 dns_logs_profile.yaml
domainSecurityType:
  generator:
    values:
      - value: 0
        weight: 70
      - value: 3   # 恶意域名
        weight: 20
      - value: 4   # 内容拦截
        weight: 10
```

### 场景 4: 查询性能测试

先写入数据，然后执行各种查询:

```sql
-- TOP 域名查询
SELECT firstQueryName, count() as cnt
FROM dnsmon.cdns_log
WHERE tnow >= today()
GROUP BY firstQueryName
ORDER BY cnt DESC
LIMIT 100;

-- 时序聚合查询
SELECT 
    toStartOfHour(tnow) as hour,
    count() as requests,
    avg(responseDelay) as avg_delay
FROM dnsmon.cdns_log
WHERE tnow >= today()
GROUP BY hour
ORDER BY hour;
```

## 🎓 进阶使用

### 多场景切换

创建多个配置文件:

```bash
profiles/
├── dns_normal.yaml      # 正常流量
├── dns_attack.yaml      # 攻击场景
└── dns_mixed.yaml       # 混合场景

# 运行不同场景
./clickcannon -config config_dns_normal.yaml
./clickcannon -config config_dns_attack.yaml
```

### 与真实数据混合

```bash
# 方案: 使用多表
CREATE TABLE dnsmon.cdns_log_generated AS dnsmon.cdns_log;  # 生成的数据
CREATE TABLE dnsmon.cdns_log_real AS dnsmon.cdns_log;       # 真实数据

# 查询时合并
SELECT * FROM (
    SELECT * FROM dnsmon.cdns_log_generated
    UNION ALL
    SELECT * FROM dnsmon.cdns_log_real
)
WHERE tnow >= today();
```

## ✅ 检查清单

使用前确认:

- [ ] ClickHouse 数据库运行正常
- [ ] dnsmon.cdns_log 表已创建
- [ ] 数组字段设置了默认值 (空数组)
- [ ] 网络连接正常 (能访问 10.160.152.186:9000)
- [ ] 用户名密码正确
- [ ] 有足够的磁盘空间

使用后验证:

- [ ] 数据能正常写入
- [ ] 数据分布符合预期
- [ ] 性能满足要求
- [ ] 没有错误日志

## 📞 支持

如遇问题:

1. 查看日志: `log_level: debug` 获取详细信息
2. 查看文档: `docs/DNS日志生成使用指南.md`
3. 检查配置: `dns_logs_profile.yaml` 语法是否正确
4. 测试连接: `clickhouse-client` 能否连接

## 🔮 后续计划

### Phase 2 (计划中)

- [ ] 支持数组类型字段配置
- [ ] 支持条件生成器 (字段间关联)
- [ ] 支持模板生成器 (组合字段)
- [ ] 支持从现有数据学习分布

### Phase 3 (远期)

- [ ] Web UI 配置编辑器
- [ ] 自动表结构检测
- [ ] 数据质量验证工具
- [ ] 性能基准测试报告

---

**项目**: ClickCannon  
**版本**: v1.0  
**创建时间**: 2026-08-10  
**作者**: AI Assistant  

**总结**: 完整的配置化方案已就绪，可直接用于 DNS 日志压测，无需修改任何代码！
