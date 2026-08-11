#!/bin/bash

# ClickHouse连接信息
CH_HOST="10.160.152.186"
CH_PORT="9000"
CH_USER="admin"
CH_PASSWORD="V%t^ckmstB"
CH_DB="clickcannon"

echo "📊 ClickCannon 监控指标查询工具"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 查询最近的运行
echo "🏃 最近的运行记录："
clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD --database=$CH_DB --query="
SELECT 
    run_id,
    run_name,
    data_type,
    formatReadableTimeDifference(now(), start_time) as started_ago,
    formatReadableSize(target_bytes_per_second) as target_rate
FROM runs 
ORDER BY start_time DESC 
LIMIT 5
FORMAT PrettyCompact"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 获取最新的run_id
LATEST_RUN=$(clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD --database=$CH_DB --query="SELECT run_id FROM runs ORDER BY start_time DESC LIMIT 1 FORMAT TSV")

if [ -z "$LATEST_RUN" ]; then
    echo "❌ 没有找到运行记录"
    exit 1
fi

echo "📈 最新运行 ($LATEST_RUN) 的实时统计："
echo ""

# 查询关键指标
clickhouse-client --host=$CH_HOST --port=$CH_PORT --user=$CH_USER --password=$CH_PASSWORD --database=$CH_DB --query="
SELECT 
    metric_name,
    formatReadableQuantity(sum(value)) as total_value,
    formatReadableQuantity(sum(value) / (max(timestamp) - min(timestamp))) as per_second
FROM perf
WHERE run_id = '$LATEST_RUN'
  AND metric_name IN (
    'insert_rows_total',
    'insert_bytes_uncompressed_total',
    'insert_bytes_compressed_total',
    'disk_bytes_uncompressed_total',
    'disk_bytes_compressed_total'
  )
GROUP BY metric_name
ORDER BY metric_name
FORMAT PrettyCompact"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "💡 提示："
echo "  - 在Grafana中查看详细图表: http://localhost:3000"
echo "  - 选择 run_id: $LATEST_RUN"
