#!/bin/bash

echo "📊 导入 ClickCannon Dashboard 到 Grafana"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Grafana API 配置
GRAFANA_URL="http://10.41.86.87:3000"
GRAFANA_USER="admin"
GRAFANA_PASS="admin"

echo "1️⃣  检查 Grafana 连接..."
if ! curl -s -u $GRAFANA_USER:$GRAFANA_PASS $GRAFANA_URL/api/health | grep -q "ok"; then
    echo "   ❌ 无法连接到 Grafana"
    exit 1
fi
echo "   ✅ Grafana 连接正常"

echo ""
echo "2️⃣  获取 ClickHouse 数据源 UID..."
DATASOURCE_UID=$(curl -s -u $GRAFANA_USER:$GRAFANA_PASS $GRAFANA_URL/api/datasources/name/ClickHouse | grep -o '"uid":"[^"]*"' | cut -d'"' -f4)
if [ -z "$DATASOURCE_UID" ]; then
    echo "   ❌ 未找到 ClickHouse 数据源"
    exit 1
fi
echo "   ✅ 数据源 UID: $DATASOURCE_UID"

echo ""
echo "3️⃣  通过 Grafana UI 导入..."
echo "   由于 grafana.json 是新格式，请手动导入："
echo ""
echo "   方法1: 使用 Grafana UI"
echo "   ────────────────────────────────"
echo "   1. 访问: $GRAFANA_URL"
echo "   2. 登录 (admin/admin)"
echo "   3. 左侧菜单 → Dashboards → New → Import"
echo "   4. 上传文件: grafana.json"
echo "   5. 选择数据源: ClickHouse"
echo "   6. 点击 Import"
echo ""
echo "   方法2: 使用 Grafana API (需要转换格式)"
echo "   ────────────────────────────────"
echo "   项目的 grafana.json 是 v2beta1 格式"
echo "   需要转换成标准 dashboard JSON"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ 数据源已配置完成"
echo ""
echo "📝 下一步:"
echo "   请按上述方法手动导入 Dashboard"
echo ""
