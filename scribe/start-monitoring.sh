#!/bin/bash

echo "🚀 启动 ClickCannon 监控系统..."
echo ""

# 检查Docker是否运行
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker未运行，请先启动Docker"
    exit 1
fi

# 启动Grafana
echo "📊 启动 Grafana..."
docker compose up -d

# 等待Grafana启动
echo "⏳ 等待 Grafana 启动..."
sleep 10

# 检查Grafana是否启动
if curl -s http://localhost:3000 > /dev/null; then
    echo "✅ Grafana 已启动"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📈 Grafana 已就绪！"
    echo ""
    echo "🌐 访问地址: http://localhost:3000"
    echo "👤 用户名: admin"
    echo "🔑 密码: admin"
    echo ""
    echo "⚠️  需要安装 ClickHouse 插件"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "📦 正在安装插件..."
    ./install-plugin.sh
else
    echo "❌ Grafana启动失败，请检查日志: docker compose logs grafana"
fi
