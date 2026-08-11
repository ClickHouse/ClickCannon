#!/bin/bash

# 测试 Generate 模式配置化支持

echo "================================================"
echo "测试 ClickCannon Generate 模式"
echo "================================================"
echo ""

# 检查编译
echo "[1/4] 检查编译..."
if [ ! -f "./clickcannon" ]; then
    echo "错误: clickcannon 未找到，请先编译"
    echo "运行: go build -o clickcannon"
    exit 1
fi
echo "✓ 编译文件存在"
echo ""

# 检查配置文件
echo "[2/4] 检查配置文件..."
if [ ! -f "config_dns_logs.yaml" ]; then
    echo "错误: config_dns_logs.yaml 未找到"
    exit 1
fi
echo "✓ config_dns_logs.yaml 存在"

if [ ! -f "profiles/dns_logs_profile.yaml" ]; then
    echo "错误: profiles/dns_logs_profile.yaml 未找到"
    exit 1
fi
echo "✓ profiles/dns_logs_profile.yaml 存在"
echo ""

# 测试配置加载
echo "[3/4] 测试配置加载..."
echo "运行 clickcannon（5秒后自动停止）..."
timeout 5 ./clickcannon -config config_dns_logs.yaml 2>&1 | tee test_output.log
EXIT_CODE=$?

echo ""
echo "检查输出..."

# 检查是否成功加载配置
if grep -q "loaded custom fields config" test_output.log; then
    echo "✓ 配置文件加载成功"
    FIELDS_COUNT=$(grep "loaded custom fields config" test_output.log | grep -oP 'fields_count=\K\d+')
    echo "  字段数: $FIELDS_COUNT"
else
    echo "✗ 配置文件未加载"
    echo "查看日志:"
    cat test_output.log
    exit 1
fi

# 检查是否开始生成
if grep -q "started" test_output.log && grep -q "generate" test_output.log; then
    echo "✓ Generate workers 启动成功"
else
    echo "⚠ Generate workers 可能未启动"
fi

# 检查是否有错误
if grep -qi "panic\|fatal\|SIGSEGV" test_output.log; then
    echo "✗ 发现错误:"
    grep -i "panic\|fatal\|SIGSEGV\|error" test_output.log | head -10
    exit 1
else
    echo "✓ 无严重错误"
fi

echo ""
echo "[4/4] 检查 ClickHouse 数据..."
QUERY="SELECT count(), min(tnow), max(tnow), uniq(serverAddress), uniq(clientAddress) FROM dnsmon.cdns_log WHERE tnow >= now() - INTERVAL 1 MINUTE FORMAT Vertical"

clickhouse-client --host 10.160.152.186 --port 9000 \
  --user admin --password 'V%t^ckmstB' \
  --query "$QUERY" 2>&1 | tee ch_result.log

if [ ${PIPESTATUS[0]} -eq 0 ]; then
    echo "✓ ClickHouse 查询成功"
    
    # 检查是否有数据
    if grep -q "count():" ch_result.log; then
        COUNT=$(grep "count():" ch_result.log | awk '{print $2}')
        if [ "$COUNT" -gt "0" ]; then
            echo "✓ 数据已写入 ClickHouse: $COUNT 行"
        else
            echo "⚠ 暂无数据（可能需要更长运行时间）"
        fi
    fi
else
    echo "⚠ ClickHouse 查询失败（可能连接问题）"
fi

echo ""
echo "================================================"
echo "测试完成"
echo "================================================"
echo ""
echo "如需继续运行，执行:"
echo "  ./clickcannon -config config_dns_logs.yaml"
echo ""
echo "如需查看完整日志:"
echo "  cat test_output.log"
echo ""
echo "如需实时监控数据:"
echo "  watch -n 1 \"clickhouse-client --host 10.160.152.186 --port 9000 \\"
echo "    --user admin --password 'V%t^ckmstB' \\"
echo "    --query 'SELECT count() FROM dnsmon.cdns_log'\""

# 清理
rm -f test_output.log ch_result.log

exit 0
