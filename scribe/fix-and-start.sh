#!/bin/bash

echo "🔧 ClickCannon 监控系统 - 网络问题修复版"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 检查是否为root
if [ "$EUID" -ne 0 ]; then 
    echo "⚠️  请使用 sudo 运行此脚本"
    echo "   sudo ./fix-and-start.sh"
    exit 1
fi

cd /home/sunchanglong/go_project/src/ClickCannon

echo "📦 步骤1/6: 停止旧容器..."
docker compose down -v 2>&1 | grep -v "attribute.*version" | grep -v "^$"

echo ""
echo "🚀 步骤2/6: 启动 Grafana 容器（不自动安装插件）..."
docker compose up -d 2>&1 | grep -v "attribute.*version" | grep -v "^$"

echo ""
echo "⏳ 步骤3/6: 等待 Grafana 启动（15秒）..."
sleep 15

echo ""
echo "🔌 步骤4/6: 在容器内安装 ClickHouse 插件..."
echo "   （如果网络慢，可能需要等待...）"

# 尝试3次安装
INSTALL_SUCCESS=0
for i in 1 2 3; do
    echo "   尝试 $i/3..."
    if docker exec clickcannon-grafana grafana cli plugins install grafana-clickhouse-datasource 2>&1 | grep -q "Installed"; then
        INSTALL_SUCCESS=1
        echo "   ✅ 插件安装成功！"
        break
    fi
    if [ $i -lt 3 ]; then
        echo "   ⚠️  失败，5秒后重试..."
        sleep 5
    fi
done

if [ $INSTALL_SUCCESS -eq 0 ]; then
    echo ""
    echo "❌ 插件安装失败（网络问题）"
    echo ""
    echo "请查看: 网络问题解决方案.md"
    echo "或手动执行:"
    echo "  sudo docker exec clickcannon-grafana grafana cli plugins install grafana-clickhouse-datasource"
    echo "  sudo docker compose restart grafana"
    exit 1
fi

echo ""
echo "🔄 步骤5/6: 重启 Grafana 使插件生效..."
docker compose restart grafana 2>&1 | grep -v "attribute.*version" | grep -v "^$"

echo ""
echo "⏳ 步骤6/6: 等待 Grafana 重启（15秒）..."
sleep 15

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# 验证
if curl -s http://localhost:3000 > /dev/null 2>&1; then
    echo "✅ Grafana 运行正常！"
    echo ""
    
    # 验证插件
    if docker exec clickcannon-grafana grafana cli plugins ls 2>&1 | grep -q "grafana-clickhouse-datasource"; then
        echo "✅ ClickHouse 插件已安装！"
    else
        echo "⚠️  插件可能未安装成功，请手动检查"
    fi
    
    echo ""
    echo "🎉 监控系统就绪！"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🌐 访问地址: http://localhost:3000"
    echo "👤 用户名: admin"
    echo "🔑 密码: admin"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "📊 下一步："
    echo "  1. 打开浏览器访问 http://localhost:3000"
    echo "  2. 登录后，在 Configuration → Data sources 中验证 ClickHouse 连接"
    echo "  3. 在另一终端运行: ./clickcannon --config config.yaml"
    echo "  4. 记录 run_id，在 Grafana Dashboard 中选择查看"
    echo ""
else
    echo "❌ Grafana 未能正常启动"
    echo ""
    echo "查看日志："
    echo "  docker compose logs grafana"
fi
