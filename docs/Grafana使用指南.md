# 📊 ClickCannon Grafana 监控使用指南

## ✅ 当前状态

- ✅ Grafana 已启动：http://10.41.86.87:3000
- ✅ ClickHouse 数据源已配置
- ✅ ClickHouse 插件已加载
- ⏳ Dashboard 需要手动导入

---

## 🚀 快速开始

### 步骤 1：访问 Grafana

打开浏览器访问：
```
http://10.41.86.87:3000
```

登录信息：
- 用户名：`admin`
- 密码：`admin`

### 步骤 2：导入 Dashboard

1. 登录后，点击左侧菜单 **☰** （汉堡菜单）
2. 选择 **Dashboards** → **New** → **Import**
3. 点击 **Upload dashboard JSON file**
4. 选择文件：`/home/sunchanglong/go_project/src/ClickCannon/grafana.json`
5. 在 **ClickHouse** 下拉框中选择：**ClickHouse**
6. 点击 **Import** 按钮

### 步骤 3：启动压测

在终端运行：
```bash
cd /home/sunchanglong/go_project/src/ClickCannon
./clickcannon --config config.yaml
```

从日志中找到 `run_id`，例如：
```
2026-07-22 15:00:00 INFO  app starting run_id=550e8400-e29b-41d4-a716-446655440000
```

### 步骤 4：查看监控

1. 在 Grafana 左侧菜单中找到 **Dashboards** → **ClickCannon**
2. 在 Dashboard 顶部找到 **Run** 下拉框
3. 选择你刚才记录的 `run_id`
4. 🎉 查看实时监控图表！

---

## 📊 Dashboard 面板说明

### Program 组（程序监控）
- **Goroutines**：协程数量
- **Generators/Readers/Inserters/Users**：各模块活跃线程
- **CPU Time**：CPU 使用时间
- **Memory Usage**：内存使用情况
- **Garbage Collector Runs**：GC 运行次数

### Disk 组（磁盘读取）
- **Disk Bytes Throughput**：磁盘字节吞吐量
  - `disk_bytes_compressed_total`：压缩数据读取量
  - `disk_bytes_uncompressed_total`：解压后数据量
- **Disk Rows Throughput**：磁盘行数吞吐量
- **Disk Bytes/s by Worker**：按 Worker 分解的读取速率

### Insert 组（数据插入）
- **Insert Bytes Throughput**：插入字节吞吐量
  - `insert_bytes_compressed_total`：发送的压缩数据
  - `insert_bytes_uncompressed_total`：原始数据大小
- **Insert Rows Throughput**：插入行数吞吐量
- **Batch Throughput**：批次提交速率
- **Insert Bytes/s by Worker**：按 Worker 分解的插入速率

---

## 🔍 常用查询

### 查看所有运行记录

```sql
SELECT 
    run_id,
    name,
    data_type,
    formatDateTime(start_time, '%Y-%m-%d %H:%i:%s') as started,
    age('second', start_time, now()) as duration_sec
FROM clickcannon.runs
ORDER BY start_time DESC
LIMIT 10
```

### 查看特定运行的指标

```sql
SELECT 
    metric_name,
    count() as samples,
    formatReadableQuantity(max(value)) as max_value,
    formatReadableQuantity(avg(value)) as avg_value
FROM clickcannon.perf
WHERE run_id = 'YOUR_RUN_ID_HERE'
GROUP BY metric_name
ORDER BY metric_name
```

### 实时吞吐量（最近1分钟）

```sql
SELECT 
    metric_name,
    formatReadableQuantity((max(value) - min(value)) / 60) as per_second
FROM clickcannon.perf
WHERE run_id = (SELECT run_id FROM clickcannon.runs ORDER BY start_time DESC LIMIT 1)
  AND metric_name IN ('insert_rows_total', 'insert_bytes_uncompressed_total', 'disk_bytes_uncompressed_total')
  AND timestamp > now() - INTERVAL 1 MINUTE
GROUP BY metric_name
```

---

## 🎯 Dashboard 变量说明

Dashboard 顶部有几个变量可以调整：

### chds (ClickHouse Datasource)
- 默认：`ClickHouse`
- 说明：选择 ClickHouse 数据源

### database
- 默认：`clickcannon`
- 说明：指标数据库名称

### run_table
- 默认：`runs`
- 说明：运行记录表

### perf_table
- 默认：`perf`
- 说明：性能指标表

### run_id
- 说明：选择要查看的运行ID
- **这是最重要的变量！** 选择你的压测运行

### metrics_interval_s
- 默认：`5m` (300秒)
- 可选：1s, 5s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h
- 说明：图表数据聚合时间间隔

---

## 📈 监控指标解释

### 吞吐量指标

| 指标名 | 含义 | 期望值 |
|--------|------|--------|
| `disk_bytes_uncompressed_total` | 从磁盘读取的原始字节数 | 持续增长 |
| `disk_bytes_compressed_total` | 从磁盘读取的压缩字节数 | 持续增长 |
| `disk_rows_total` | 从磁盘读取的总行数 | 持续增长 |
| `insert_bytes_uncompressed_total` | 插入的原始字节数 | 持续增长 |
| `insert_bytes_compressed_total` | 插入的压缩字节数 | 持续增长 |
| `insert_rows_total` | 插入的总行数 | 持续增长 |
| `insert_batches_total` | 提交的批次数 | 持续增长 |

### 压缩比

```
compression_ratio = disk_bytes_uncompressed / disk_bytes_compressed
```

正常范围：2x ~ 10x（取决于数据类型）

### 错误指标

| 指标名 | 含义 | 期望值 |
|--------|------|--------|
| `insert_errors_total` | 插入错误总数 | 应该为 0 |
| `queries_failed_total` | 查询失败总数 | 应该为 0 |

---

## 🔧 故障排查

### Dashboard 没有数据

**可能原因1：run_id 未选择或错误**
- 确认顶部 **Run** 下拉框已选择正确的 run_id
- 从压测日志中复制完整的 run_id

**可能原因2：时间范围不对**
- 点击右上角时间选择器
- 选择 **Last 15 minutes** 或更大范围
- 确保时间范围覆盖你的压测运行时间

**可能原因3：数据还没写入**
- 等待几秒钟，数据写入有延迟
- 检查 ClickHouse 中是否有数据：
  ```bash
  ./check-metrics.sh
  ```

### 数据源连接失败

检查 ClickHouse 连接：
```bash
clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' --query="SELECT 1"
```

如果失败，检查：
1. ClickHouse 服务是否运行
2. 网络连接是否正常
3. 用户名密码是否正确

### 图表显示 "No data"

1. 检查变量配置：
   - `database` = `clickcannon`
   - `run_table` = `runs`
   - `perf_table` = `perf`

2. 手动查询验证：
   ```sql
   SELECT count() FROM clickcannon.perf 
   WHERE run_id = 'YOUR_RUN_ID'
   ```

3. 检查压测是否启用了指标：
   ```yaml
   # config.yaml
   metrics:
     enabled: true  # 必须为 true
   ```

---

## 💡 使用技巧

### 1. 实时刷新

点击右上角的刷新按钮，选择自动刷新间隔：
- 推荐：**5s** 或 **10s**

### 2. 对比多次运行

1. 复制 Dashboard（右上角 ⚙️  → Save as）
2. 在两个 Dashboard 中选择不同的 run_id
3. 并排查看对比

### 3. 放大查看细节

- 在图表上拖动鼠标选择时间范围
- 点击 **Zoom out** 恢复

### 4. 导出数据

- 点击面板标题 → **Inspect** → **Data**
- 可以下载 CSV 或 JSON 格式

### 5. 创建告警

1. 编辑面板 → **Alert** 标签
2. 设置告警条件（如错误率 > 0）
3. 配置通知渠道

---

## 📝 配置文件说明

### config.yaml - 指标配置

```yaml
metrics:
  enabled: true                              # 启用指标收集
  clickhouse_dsn: tcp://10.160.152.186:9000 # ClickHouse 地址
  database: clickcannon                      # 指标数据库
  run_table: runs                            # 运行记录表
  metrics_table: perf                        # 性能指标表
  create_schema: true                        # 自动创建表
  attributes:                                # 自定义标签
    env: "production"
    team: "your-team"
```

### grafana/provisioning/datasources/clickhouse.yml

```yaml
apiVersion: 1
datasources:
  - name: ClickHouse
    type: grafana-clickhouse-datasource
    url: http://10.160.152.186:9000
    jsonData:
      defaultDatabase: clickcannon
      port: 9000
      protocol: native
      server: 10.160.152.186
      username: admin
    secureJsonData:
      password: V%t^ckmstB
```

---

## 🆘 获取帮助

### 命令行工具

```bash
# 快速查看指标
./check-metrics.sh

# 验证监控系统
./验证监控.sh

# 导入 Dashboard
./导入dashboard.sh
```

### 相关文档

- [MONITORING.md](./MONITORING.md) - 监控系统详解
- [QUICKSTART.md](./QUICKSTART.md) - 快速开始指南
- [README.md](./README.md) - 项目总览

### 查看日志

```bash
# Grafana 日志
sudo docker logs clickcannon-grafana

# ClickCannon 日志
tail -f clickcannon.log
```

---

## ✅ 检查清单

使用此清单确认所有配置正确：

- [ ] Grafana 可访问（http://10.41.86.87:3000）
- [ ] 能用 admin/admin 登录
- [ ] ClickHouse 数据源已配置
- [ ] Dashboard 已导入
- [ ] config.yaml 中 `metrics.enabled: true`
- [ ] ClickHouse 连接正常
- [ ] 压测程序正在运行
- [ ] 在 Dashboard 中选择了正确的 run_id
- [ ] 图表显示数据（不是 "No data"）

---

## 🎉 完成！

现在你可以：

1. ✅ 实时监控压测性能
2. ✅ 分析 Disk 和 Insert 吞吐量
3. ✅ 追踪系统资源使用
4. ✅ 对比多次压测结果
5. ✅ 导出数据用于报告

Happy Monitoring! 📊🚀
