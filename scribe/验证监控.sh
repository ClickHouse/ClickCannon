#!/bin/bash

echo "🔍 ClickCannon 监控系统验证"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

echo "1️⃣  检查 Grafana 状态..."
if curl -s http://10.41.86.87:3000/api/health | grep -q "ok"; then
    echo "   ✅ Grafana 运行正常"
else
    echo "   ❌ Grafana 未运行"
    exit 1
fi

echo ""
echo "2️⃣  检查数据源..."
DATASOURCE=$(curl -s -u admin:admin http://10.41.86.87:3000/api/datasources | grep -o '"name":"ClickHouse"' | wc -l)
if [ "$DATASOURCE" -gt 0 ]; then
    echo "   ✅ ClickHouse 数据源已配置"
else
    echo "   ⚠️  数据源可能未配置"
fi

echo ""
echo "3️⃣  检查 Dashboard..."
DASHBOARD=$(curl -s -u admin:admin http://10.41.86.87:3000/api/search?query=ClickCannon | grep -o '"title":"ClickCannon"' | wc -l)
if [ "$DASHBOARD" -gt 0 ]; then
    echo "   ✅ ClickCannon Dashboard 已导入"
    
    # 获取 Dashboard UID
    DASH_UID=$(curl -s -u admin:admin http://10.41.86.87:3000/api/search?query=ClickCannon | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4)
    echo "   Dashboard UID: $DASH_UID"
else
    echo "   ⚠️  Dashboard 可能未导入"
fi

echo ""
echo "4️⃣  检查 ClickHouse 连接..."
if clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' \
  --query="SELECT 1" 2>/dev/null | grep -q "1"; then
    echo "   ✅ ClickHouse 连接正常"
else
    echo "   ⚠️  ClickHouse 连接失败"
fi

echo ""
echo "5️⃣  检查监控表..."
RUNS_TABLE=$(clickhouse-client --host=10.160.152.186 --port=9000 \
  --user=admin --password='V%t^ckmstB' \
  --query="EXISTS TABLE clickcannon.runs" 2>/dev/null)
if [ "$RUNS_TABLE" == "1" ]; then
    echo "   ✅ runs 表存在"
    
    # 查询最近的运行
    RECENT_RUNS=$(clickhouse-client --host=10.160.152.186 --port=9000 \
      --user=admin --password='V%t^ckmstB' \
      --query="SELECT count() FROM clickcannon.runs" 2>/dev/null)
    echo "   总运行次数: $RECENT_RUNS"
else
    echo "   ⚠️  runs 表不存在（首次运行后会创建）"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎯 访问信息"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "🌐 Grafana URL:"
echo "   http://10.41.86.87:3000"
echo ""
echo "👤 登录信息:"
echo "   用户: admin"
echo "   密码: admin"
echo ""
if [ ! -z "$DASH_UID" ]; then
    echo "📊 Dashboard 直达链接:"
    echo "   http://10.41.86.87:3000/d/$DASH_UID/clickcannon"
    echo ""
fi
echo "💡 使用步骤:"
echo "   1. 在另一个终端运行: ./clickcannon --config config.yaml"
echo "   2. 从日志中复制 run_id"
echo "   3. 在 Grafana Dashboard 顶部选择 run_id"
echo "   4. 查看实时监控图表"
echo ""
