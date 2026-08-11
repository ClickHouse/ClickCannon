#!/bin/bash

echo "📦 安装 Grafana ClickHouse 插件..."
echo ""

# 检查容器是否运行
if ! docker ps | grep -q clickcannon-grafana; then
    echo "❌ Grafana 容器未运行，请先执行: docker compose up -d"
    exit 1
fi

echo "🔧 在容器内安装插件..."
docker exec clickcannon-grafana grafana cli plugins install grafana-clickhouse-datasource

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ 插件安装成功！"
    echo "🔄 重启 Grafana 容器..."
    docker compose restart grafana
    
    echo ""
    echo "⏳ 等待 Grafana 重启..."
    sleep 10
    
    if curl -s http://localhost:3000 > /dev/null; then
        echo "✅ Grafana 已重启"
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "🎉 监控系统完全就绪！"
        echo ""
        echo "🌐 访问: http://localhost:3000"
        echo "👤 用户: admin"
        echo "🔑 密码: admin"
        echo ""
        echo "💡 提示："
        echo "  1. 在另一个终端运行: ./clickcannon --config config.yaml"
        echo "  2. 从日志中复制 run_id"
        echo "  3. 在Grafana中选择对应的 run_id 查看实时指标"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    else
        echo "⚠️  Grafana 重启中，请稍后访问 http://localhost:3000"
    fi
else
    echo ""
    echo "❌ 插件安装失败（可能是网络问题）"
    echo ""
    echo "🔧 备选方案：手动下载插件"
    echo ""
    echo "1. 下载插件到本地："
    echo "   wget https://github.com/grafana/clickhouse-datasource/releases/download/v4.5.3/grafana-clickhouse-datasource-4.5.3.linux_amd64.zip"
    echo ""
    echo "2. 解压到 Grafana 插件目录："
    echo "   unzip grafana-clickhouse-datasource-4.5.3.linux_amd64.zip -d /tmp/"
    echo "   docker cp /tmp/grafana-clickhouse-datasource clickcannon-grafana:/var/lib/grafana/plugins/"
    echo ""
    echo "3. 重启容器："
    echo "   docker compose restart grafana"
fi
