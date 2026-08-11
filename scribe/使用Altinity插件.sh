#!/bin/bash

echo "🔧 使用 Altinity ClickHouse 插件（更容易安装）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ "$EUID" -ne 0 ]; then 
    echo "⚠️  请使用 sudo 运行"
    exit 1
fi

cd /home/sunchanglong/go_project/src/ClickCannon

echo "1️⃣  确保 Grafana 正在运行..."
if ! docker ps | grep -q clickcannon-grafana; then
    echo "   启动 Grafana..."
    docker compose up -d
    sleep 20
fi

echo ""
echo "2️⃣  安装 Altinity ClickHouse 插件..."
echo "   （这是一个替代的 ClickHouse 数据源插件）"
docker exec clickcannon-grafana grafana cli plugins install vertamedia-clickhouse-datasource

echo ""
echo "3️⃣  重启 Grafana..."
docker compose restart grafana
sleep 15

echo ""
echo "4️⃣  验证..."
if docker exec clickcannon-grafana grafana cli plugins ls 2>&1 | grep -q "vertamedia-clickhouse-datasource"; then
    echo "   ✅ Altinity 插件已安装"
    
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🎉 完成！"
    echo ""
    echo "🌐 访问: http://localhost:3000"
    echo "👤 用户: admin / admin"
    echo ""
    echo "⚠️  注意：需要手动配置数据源"
    echo ""
    echo "📝 配置步骤："
    echo "  1. 登录 Grafana"
    echo "  2. 左侧菜单 → Connections → Data sources"
    echo "  3. 点击 'Add data source'"
    echo "  4. 搜索 'ClickHouse' (Altinity)"
    echo "  5. 配置:"
    echo "     - URL: http://10.160.152.186:8123"
    echo "     - Auth: 输入用户名密码"
    echo "     - Default database: clickcannon"
    echo "  6. 点击 'Save & test'"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
else
    echo "   ❌ 安装失败"
    docker compose logs grafana | tail -20
fi
