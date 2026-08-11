# 🚀 ClickCannon 压测监控 - 快速启动指南

## 📋 前置条件检查

- ✅ ClickHouse 运行在 `10.160.152.186:9000`
- ✅ Docker 已安装并运行
- ✅ 数据文件 `log_data/100w.native.zst` 已准备好
- ✅ 已编译 `./clickcannon` 二进制文件

## 🎯 三步启动完整监控

### 第 1 步：启动 Grafana 监控系统

```bash
cd /home/sunchanglong/go_project/src/ClickCannon
./start-monitoring.sh
```

**预期输出：**
```
✅ Grafana 已启动
🌐 访问地址: http://localhost:3000
👤 用户名: admin
🔑 密码: admin
```

### 第 2 步：启动压测（在新终端）

```bash
cd /home/sunchanglong/go_project/src/ClickCannon
./clickcannon --config config.yaml
```

**预期输出：**
```
2026-07-22 12:10:00 INFO  app starting run_id=abc123-def456-ghi789
2026-07-22 12:10:00 INFO  metrics schema created
2026-07-22 12:10:01 INFO  disk worker 0 started
2026-07-22 12:10:01 INFO  insert worker 0 started
2026-07-22 12:10:02 INFO  disk loop_index=0 file=100w.native.zst
```

**⚠️ 重要：复制 `run_id` 值！**

### 第 3 步：查看监控

1. 打开浏览器：**http://localhost:3000**
2. 登录：`admin` / `admin`
3. 点击左侧菜单 → **Dashboards** → **ClickCannon**
4. 在顶部变量选择器中，选择你的 **run_id**
5. 实时查看监控指标！📊

## 📊 监控面板展示内容

### 关键指标（实时更新）

| 面板名称 | 指标说明 | 预期值 |
|---------|---------|--------|
| **Disk Bytes Throughput** | 磁盘读取速率 | ~8-10 GiB/s |
| **Insert Rows Throughput** | 插入行数/秒 | 取决于数据 |
| **Insert Bytes Throughput** | 插入字节/秒 | ~8-10 GiB/s |
| **Compression Ratio** | 压缩比 | 2-5x |
| **Insert Errors** | 插入错误数 | 应该为 0 |

### Dashboard 截图位置
- Disk & Insert 面板：`.static/grafana_disk_and_insert_dashboard.png`
- User Query 面板：`.static/grafana_user_dashboard.png`

## 🔍 命令行快速查看

如果不想打开浏览器，可以用命令行查看：

```bash
./check-metrics.sh
```

输出示例：
```
📊 最新运行的实时统计：
┌─metric_name────────────────────┬─total_value─┬─per_second──┐
│ insert_rows_total              │ 5.00 million│ 16.67 thousand│
│ insert_bytes_uncompressed_total│ 2.50 GiB    │ 8.33 MiB    │
└────────────────────────────────┴─────────────┴─────────────┘
```

## 🎛️ 当前配置说明

```yaml
# 压测配置
disk:
  threads: 8                      # 8个读取线程
  loop: true                      # 持续循环
  mb_per_second_limit: 10240      # 10 GiB/s 限速

insert:
  threads: 8                      # 8个插入线程
  batch_size: 100000              # 每批10万行

# 监控配置
metrics:
  enabled: true                   # 启用监控
  database: clickcannon           # 指标存储数据库
  create_schema: true             # 自动创建表
```

## 🛑 停止服务

### 停止压测
按 `Ctrl+C` 停止 clickcannon

### 停止监控
```bash
docker-compose down
```

## 📈 性能调优提示

### 提高吞吐量
```yaml
disk:
  threads: 16                     # 增加到16线程
  mb_per_second_limit: 20480      # 提高到20 GiB/s

insert:
  threads: 16
  batch_size: 200000              # 增大批次
```

### 降低资源使用
```yaml
disk:
  threads: 4                      # 减少线程
  mb_per_second_limit: 2048       # 限制到2 GiB/s

insert:
  threads: 4
  batch_size: 50000
```

## 🔧 常见问题

### Q1: Grafana 无法启动
```bash
# 检查端口占用
sudo netstat -tlnp | grep 3000

# 如果被占用，修改 docker-compose.yml 中的端口
# ports: - "3001:3000"
```

### Q2: Dashboard 没有数据
1. 确认 `config.yaml` 中 `metrics.enabled: true`
2. 确认在 Grafana 中选择了正确的 run_id
3. 运行 `./check-metrics.sh` 验证数据是否写入

### Q3: 压测速度太慢
- 检查 `mb_per_second_limit` 是否设置过低
- 增加 `threads` 数量
- 增大 `batch_size`

### Q4: ClickHouse 连接失败
```bash
# 测试连接
telnet 10.160.152.186 9000

# 检查用户名密码
clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' --query="SELECT 1"
```

## 📚 详细文档

- **监控系统详解**: [MONITORING.md](MONITORING.md)
- **项目完整文档**: [README.md](README.md)
- **配置说明**: [docs/config.md](docs/config.md)

## ✅ 验证清单

完成以下步骤即代表部署成功：

- [ ] Grafana 访问 http://localhost:3000 正常
- [ ] 登录成功（admin/admin）
- [ ] Dashboard 自动导入成功
- [ ] clickcannon 启动无报错
- [ ] 在 Grafana 中能看到 run_id
- [ ] Dashboard 显示实时数据
- [ ] 各项指标正常增长

## 🎉 完成！

现在你有了一个完整的压测监控系统：
- ✅ 持续循环压测
- ✅ 实时监控指标
- ✅ 可视化Dashboard
- ✅ 历史数据追踪

开始享受你的压测之旅吧！🚀
