# ClickCannon 配置项说明

本文档用于说明当前版本中所有可用配置项的作用，内容基于项目中的配置结构定义和示例配置文件 [example.yaml](../example.yaml)。

## 配置文件结构总览

配置文件是 YAML 格式，根节点包含以下主配置段：

- app：应用基础设置
- pprof：性能分析端点设置
- generate：生成式数据源设置（合成数据）
- disk：磁盘回放数据源设置（回放已有数据文件）
- insert：写入 ClickHouse 的设置
- otel：导出为 OTLP 的设置
- metrics：内部指标采集与写入设置
- user：用户查询负载模拟设置

> 注意：generate 和 disk 互斥，不能同时启用；insert 和 otel 也不能同时作为双写目标。

---

## 1. app

应用层基础配置。

- name：运行实例的名字，用于在日志或指标中区分不同任务。
- log_level：日志级别，常见取值如 debug、info、warn、error。
- log_to_console：是否把日志输出到控制台。
- log_to_file：是否把日志写入文件。
- data_type：当前处理的数据类型，支持以下值：
  - logs：日志数据
  - traces：链路追踪数据
  - profiles：性能剖析数据
- seed：随机数生成器种子，用于让测试结果具有可复现性。

---

## 2. pprof

性能分析相关配置。

- address：pprof HTTP 服务监听地址。
  - 留空或省略时表示关闭 pprof。
  - 例如 localhost:6060。

---

## 3. generate

生成式数据源配置，用于直接生成合成数据，而不是从磁盘读取已有文件。

- enabled：是否启用生成模式。
- threads：生成线程数。
- rows_per_block：每个数据块中生成多少行，生成后会送入插入队列。
- rows_per_second：整体生成速率限制，单位为行/秒。
  - 0 表示不限制。
- reuse_blocks：是否复用内存块，能提升吞吐并减少分配开销。
- block_retirement_uses：复用块在被回收前最多使用次数。
  - 0 表示禁用回收，块会一直保留。
- profile：选择内置生成配置文件（profile）。
  - 例如 otel_demo。
- traces：仅在 data_type 为 traces 时生效。
  - spans_per_trace_min：单条 trace 中最少 span 数。
  - spans_per_trace_max：单条 trace 中最多 span 数。
  - max_depth：trace 树的最大深度。
  - duration_min_us：span 持续时间最小值，单位为微秒。
  - duration_max_us：span 持续时间最大值，单位为微秒。
- profiles：仅在 data_type 为 profiles 时生效。
  - stack_depth_min：单个 sample 的最小堆栈深度。
  - stack_depth_max：单个 sample 的最大堆栈深度。
  - duration_min_ms：profile 持续时间最小值，单位为毫秒。
  - duration_max_ms：profile 持续时间最大值，单位为毫秒。
  - period_ns：采样周期，单位为纳秒。

---

## 4. disk

磁盘回放模式配置，用于读取预导出的 .native 或 .native.zst 数据文件。

- enabled：是否启用磁盘回放模式。
- threads：读取线程数。
- logs_path：日志数据文件目录。
- traces_path：链路追踪数据文件目录。
- profiles_path：性能剖析数据文件目录。
- reuse_blocks：是否复用读取块，减少内存分配。
- block_retirement_uses：复用块最大使用次数。
- loop：是否循环播放数据文件队列。
- mb_per_second_limit：读取时的解压后数据限速，单位为 MiB/s。
- shift_timestamp：重放时对时间戳进行偏移。
  - none：不做任何处理
  - date：只替换日期部分
  - all：按原始时间间隔重放
  - now：将时间戳改为当前时间
- has_timestamp_time：磁盘文件中是否包含 TimestampTime 列。

---

## 5. insert

将数据写入 ClickHouse 的配置。

- enabled：是否启用插入模式。
- threads：插入线程数。
- batch_size：每次 INSERT 语句发送的最大行数。
  - 实际上是一个目标上限，不保证严格等于该值。
- worker_retirement_batches：每个插入 worker 发送多少批次后重建连接，防止内存持续增长。
  - 0 表示禁用重建。
- balance_nodes：是否让连接均衡分布到集群节点。
- clickhouse：ClickHouse 连接相关配置。
  - address：ClickHouse 地址，格式如 localhost:9000。
  - secure：是否启用安全连接。
  - compression：压缩算法，例如 lz4、zstd 等。
  - user：用户名。
  - password：密码。
  - database：目标数据库。
  - logs_table：日志表名。
  - traces_table：链路表名。
  - profiles_table：性能剖析表名。

---

## 6. otel

将数据通过 OTLP/gRPC 导出到 OpenTelemetry Collector 的配置。

- enabled：是否启用 OTEL 导出。
- url：目标 OTLP/gRPC 地址。
- insecure：是否禁用 TLS，使用明文 gRPC。
- threads：导出线程数。
- batch_size：累计到多少行后触发一次导出。
- flush_interval：批次在未满时等待多长时间后强制刷新。
- timeout：单次导出请求的超时时间。
- compression：gRPC 压缩方式，常见值为 none 或 gzip。
- headers：附加到每次请求的元数据头，例如认证信息。

---

## 7. metrics

程序自身性能指标采集配置。

- enabled：是否启用指标采集。
- clickhouse_dsn：用于写入指标的 ClickHouse DSN。
- database：指标数据库名。
- run_table：运行信息表名。
- metrics_table：指标数据表名。
- create_schema：是否在启动时自动创建相关表结构。
- attributes：附加到运行记录和指标点的标签集合，适合标注环境、团队或机器信息。

---

## 8. user

用户查询负载模拟配置，用于对 ClickHouse 进行查询压力测试。

- enabled：是否启用用户负载模拟。
- duration：模拟运行总时长。
- threads：并发模拟用户数。
- ramp_duration：所有用户启动所需的时间。
- clickhouse_dsn：查询所使用的 ClickHouse DSN。
- connections_per_thread：每个线程建立多少个连接。
- database：查询使用的数据库。
- table：查询使用的表。
- dataset_unix_start：数据集开始时间（Unix 时间戳）。
- dataset_unix_end：数据集结束时间（Unix 时间戳）。
- workflows：用户工作流列表。

### 8.1 workflows

单个工作流定义了一个查询负载模式。

- type：工作流类型，目前主要使用 queries。
- name：工作流名称，用于指标和日志标识。

#### queries 工作流

- random：是否按随机顺序执行查询。
- think_time：两次查询之间的等待时间。
  - min：最小等待时间。
  - max：最大等待时间。
- time_anchor：时间范围的锚点。
  - now：使用当前时间
  - dataset_end：使用数据集结束时间
  - dataset_random：在数据集区间内随机选择一个时间点
- default_time_range：默认时间范围设置。
  - type：时间范围类型，支持 none、fixed、uniform、exponential、log_normal。
  - round：对采样结果进行取整。
  - value/lookback：固定时间范围的回看时长。
  - min/max：均匀分布时的最小/最大范围。
  - mean：指数/对数正态分布时的均值。
  - sigma：对数正态分布的离散度参数。
- default_settings：默认 ClickHouse 查询设置。
- time_range_cadence：默认时间范围的重采样频率。
  - per_query：每次查询都重新采样
  - per_loop：每轮循环只采样一次
- vars：工作流级别的变量集合，查询中可直接使用。
- preflight_cadence：预检查询的执行节奏。
  - once：只执行一次
  - per_loop：每轮循环执行一次
  - per_query：每次查询前执行一次
- preflight_queries：工作流级别预检查询。
  - sql：SQL 语句。
  - binds：查询结果绑定名称，用于后续查询引用。
  - settings：查询设置。
- queries：主查询列表。
  - name：查询名称。
  - sql：SQL 语句。
  - perf：性能目标配置，用于记录延迟目标。
    - p50/p90/p95/p99：对应百分位性能目标。
  - time_range：查询级别的时间范围覆盖。
  - vars：查询级别变量。
  - preflight_queries：查询级别预检查询。
  - settings：查询级别设置。

#### har 工作流

- file：HAR 文件路径。
- think_time：查询间等待时间。

---

## 额外说明

- 如果你需要快速查看所有可配置项的完整示例，可以直接参考 [example.yaml](../example.yaml)。
- 文档中的很多字段会在启动时进行校验，缺少必需项或取值不合法时会直接报错。
- 生成模式和磁盘回放模式是两套互斥的数据源，实际使用时需要选择其中一种。
