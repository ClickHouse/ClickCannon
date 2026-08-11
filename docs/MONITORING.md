# 📊 ClickCannon 监控系统使用指南

## 🎯 概述

本监控系统基于 **Grafana + ClickHouse**，提供实时的压测指标可视化。

## 📦 系统组成

```
监控架构:
┌─────────────────┐
│  ClickCannon    │ ──> 生成指标数据
│  (压测工具)     │
└────────┬────────┘
         │ 写入指标
         ↓
┌─────────────────┐
│  ClickHouse     │ ──> 存储指标数据
│  (指标数据库)   │     (clickcannon.runs)
└────────┬────────┘     (clickcannon.perf)
         │ 查询
         ↓
┌─────────────────┐
│    Grafana      │ ──> 可视化展示
│  (监控面板)     │     http://localhost:3000
└─────────────────┘
```

## 🚀 快速开始

### 1️⃣ 启动监控系统

```bash
cd /home/sunchanglong/go_project/src/ClickCannon

# 启动 Grafana（Docker方式）
./start-monitoring.sh
```

预期输出：
```
🚀 启动 ClickCannon 监控系统...
📊 启动 Grafana...
⏳ 等待 Grafana 启动...
✅ Grafana 已启动

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📈 监控系统已就绪！

🌐 访问地址: http://localhost:3000
👤 用户名: admin
🔑 密码: admin
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### 2️⃣ 启动压测

在**另一个终端**运行：

```bash
cd /home/sunchanglong/go_project/src/ClickCannon

# 启动持续压测
./clickcannon --config config.yaml
```

**关键信息：记录 run_id**
```
2026-07-22 12:05:23 INFO  app starting run_id=abc123-def456-ghi789
                                      ↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑
                                      复制这个 run_id
```

### 3️⃣ 查看监控面板

1. 打开浏览器访问: **http://localhost:3000**
2. 登录（admin/admin）
3. 左侧菜单 → Dashboards → **ClickCannon**
4. 在顶部选择你的 `run_id`
5. 实时查看压测指标！

## 📈 监控指标说明

### Disk 模块指标
- **Disk Bytes Throughput**: 从磁盘读取的字节速率
  - `disk_bytes_compressed_total`: 压缩数据读取量
  - `disk_bytes_uncompressed_total`: 解压后数据量
- **Disk Rows Throughput**: 从磁盘读取的行数速率
- **Disk Blocks Throughput**: 读取的数据块速率

### Insert 模块指标
- **Insert Bytes Throughput**: 插入到ClickHouse的字节速率
  - `insert_bytes_compressed_total`: 发送的压缩数据
  - `insert_bytes_uncompressed_total`: 原始数据大小
- **Insert Rows Throughput**: 插入的行数速率
- **Insert Batches Throughput**: 批次提交速率
- **Insert Errors**: 插入失败次数

### 性能指标
- **Compression Ratio**: 压缩比（越高越好）
- **Memory Usage**: 内存使用情况
- **Goroutines**: 并发协程数
- **GC Pauses**: 垃圾回收暂停时间

## 🔍 命令行查询指标

如果想快速查看指标（不打开Grafana），运行：

```bash
./check-metrics.sh
```

输出示例：
```
📊 ClickCannon 监控指标查询工具
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

🏃 最近的运行记录：
┌─run_id───────┬─run_name────────┬─data_type─┬─started_ago─┬─target_rate─┐
│ abc123...    │ insert logs     │ logs      │ 5 minutes   │ 10.00 GiB   │
└──────────────┴─────────────────┴───────────┴─────────────┴─────────────┘

📈 最新运行的实时统计：
┌─metric_name────────────────────┬─total_value─┬─per_second──┐
│ insert_rows_total              │ 5.00 million│ 16.67 thousand│
│ insert_bytes_uncompressed_total│ 2.50 GiB    │ 8.33 MiB    │
│ disk_bytes_compressed_total    │ 1.20 GiB    │ 4.00 MiB    │
└────────────────────────────────┴─────────────┴─────────────┘
```

## ⚙️ 配置文件

### config.yaml - 指标配置
```yaml
metrics:
  enabled: true                              # 启用指标收集
  clickhouse_dsn: tcp://10.160.152.186:9000 # ClickHouse地址
  database: clickcannon                      # 指标数据库
  run_table: runs                            # 运行记录表
  metrics_table: perf                        # 性能指标表
  create_schema: true                        # 自动创建表结构
  attributes:                                # 自定义标签
    env: "dev"
    foo: "bar"
```

### docker-compose.yml - Grafana部署
```yaml
services:
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_INSTALL_PLUGINS=grafana-clickhouse-datasource  # 自动安装ClickHouse插件
    volumes:
      - ./grafana-provisioning:/etc/grafana/provisioning  # 自动配置
```

## 🛠️ 常见操作

### 停止监控系统
```bash
docker-compose down
```

### 查看 Grafana 日志
```bash
docker-compose logs -f grafana
```

### 重启 Grafana
```bash
docker-compose restart grafana
```

### 查看 ClickHouse 中的指标表
```bash
clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' \
  --query="SELECT * FROM clickcannon.runs ORDER BY start_time DESC LIMIT 5"

clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' \
  --query="SELECT metric_name, count() FROM clickcannon.perf GROUP BY metric_name"
```

## 📊 Dashboard 面板说明

Grafana Dashboard 包含以下面板组：

### 1. Disk & Insert Overview
- 总体吞吐量趋势
- 压缩比监控
- 错误率统计

### 2. Detailed Metrics
- 按线程分解的性能
- 批次大小分布
- 延迟百分位数

### 3. System Resources
- CPU使用率
- 内存消耗
- 网络IO

### 4. ClickHouse Performance
- 插入QPS
- 合并活动
- 表大小增长

## 🎯 性能调优建议

根据监控指标调整配置：

### 如果 Disk 吞吐量 < Insert 吞吐量
→ 磁盘读取是瓶颈
```yaml
disk:
  threads: 16              # 增加读取线程
  mb_per_second_limit: 20480  # 提高限速
```

### 如果 Insert 吞吐量 < Disk 吞吐量  
→ 插入是瓶颈
```yaml
insert:
  threads: 16              # 增加插入线程
  batch_size: 200000       # 增大批次
```

### 如果内存持续增长
→ 需要启用对象回收
```yaml
disk:
  block_retirement_uses: 100  # 启用block回收

insert:
  worker_retirement_batches: 100  # 启用worker回收
```

### 如果出现大量错误
→ 降低并发或批次大小
```yaml
insert:
  threads: 4               # 减少线程
  batch_size: 50000        # 减小批次
```

## 🔧 故障排查

### Grafana 无法连接 ClickHouse
1. 检查 ClickHouse 是否可访问：
   ```bash
   telnet 10.160.152.186 9000
   ```
2. 检查数据源配置：
   - Grafana → Configuration → Data Sources → ClickHouse
   - 测试连接

### Dashboard 没有数据
1. 确认 `config.yaml` 中 `metrics.enabled: true`
2. 检查 run_id 是否正确
3. 查询指标表：
   ```bash
   ./check-metrics.sh
   ```

### Docker 无法启动
```bash
# 检查端口占用
sudo netstat -tlnp | grep 3000

# 查看详细日志
docker-compose logs grafana
```

## 📚 参考资料

- [Grafana官方文档](https://grafana.com/docs/)
- [ClickHouse Grafana插件](https://grafana.com/grafana/plugins/grafana-clickhouse-datasource/)
- [ClickCannon项目文档](./README.md)

## 🎉 完成！

现在你已经拥有完整的监控系统：
- ✅ 实时指标收集
- ✅ Grafana可视化面板
- ✅ 历史数据查询
- ✅ 性能趋势分析

Happy Testing! 🚀
