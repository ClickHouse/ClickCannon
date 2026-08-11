#!/bin/bash

echo "🚀 ClickCannon 监控 - 简易启动（自动处理插件）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 检查是否为root
if [ "$EUID" -ne 0 ]; then 
    echo "⚠️  请使用 sudo 运行"
    echo "   sudo ./简易启动.sh"
    exit 1
fi

cd /home/sunchanglong/go_project/src/ClickCannon

echo "1️⃣  停止旧容器..."
docker compose down -v 2>/dev/null

echo ""
echo "2️⃣  启动 Grafana..."
docker compose up -d

echo ""
echo "3️⃣  等待启动（20秒）..."
sleep 20

echo ""
echo "4️⃣  安装 ClickHouse 插件..."
echo "    （grafana-cli 会自动下载最新兼容版本）"
echo ""

# 方法1：使用 grafana-cli（推荐，自动找最新版本）
echo "    尝试方法1: grafana-cli install..."
if docker exec clickcannon-grafana grafana cli plugins install grafana-clickhouse-datasource 2>&1 | tee /tmp/plugin-install.log | grep -q "Installed\|already installed"; then
    echo "    ✅ 插件安装成功（方法1）"
    PLUGIN_INSTALLED=1
else
    echo "    ⚠️  方法1失败，可能是网络问题"
    PLUGIN_INSTALLED=0
fi

# 如果方法1失败，尝试方法2：从 grafana.com 安装
if [ $PLUGIN_INSTALLED -eq 0 ]; then
    echo ""
    echo "    尝试方法2: 从 grafana.com 安装..."
    if docker exec clickcannon-grafana grafana cli --pluginUrl https://grafana.com/api/plugins/grafana-clickhouse-datasource/versions/latest/download plugins install grafana-clickhouse-datasource 2>&1 | grep -q "Installed"; then
        echo "    ✅ 插件安装成功（方法2）"
        PLUGIN_INSTALLED=1
    else
        echo "    ⚠️  方法2也失败"
    fi
fi

# 如果都失败，显示帮助
if [ $PLUGIN_INSTALLED -eq 0 ]; then
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "❌ 自动安装失败（网络限制）"
    echo ""
    echo "🔧 手动安装方案："
    echo ""
    echo "方案A: 在 Grafana Web UI 中手动安装"
    echo "  1. 访问 http://localhost:3000"
    echo "  2. 登录 (admin/admin)"
    echo "  3. 左侧菜单 → Administration → Plugins and data"
    echo "  4. 搜索 'ClickHouse'"
    echo "  5. 点击 Install 按钮"
    echo ""
    echo "方案B: 使用 Altinity 的 ClickHouse 插件（替代品）"
    echo "  sudo docker exec clickcannon-grafana grafana cli plugins install vertamedia-clickhouse-datasource"
    echo ""
    echo "方案C: 查看详细离线安装方法"
    echo "  cat 网络问题解决方案.md"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "⚠️  即使插件未安装，Grafana 仍可访问："
    echo "    http://localhost:3000 (admin/admin)"
    exit 0
fi

echo ""
echo "5️⃣  重启 Grafana..."
docker compose restart grafana

echo ""
echo "6️⃣  等待重启（15秒）..."
sleep 15

echo ""
echo "7️⃣  验证安装..."
if docker exec clickcannon-grafana grafana cli plugins ls 2>&1 | grep -q "grafana-clickhouse-datasource"; then
    echo "    ✅ ClickHouse 插件已安装"
    PLUGIN_OK=1
else
    echo "    ⚠️  插件可能未正确安装"
    PLUGIN_OK=0
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if curl -s http://localhost:3000 > /dev/null 2>&1; then
    echo "✅ Grafana 运行正常"
    
    if [ $PLUGIN_OK -eq 1 ]; then
        echo "✅ ClickHouse 插件已安装"
        echo ""
        echo "🎉 监控系统完全就绪！"
    else
        echo "⚠️  插件需要手动安装（见上方说明）"
        echo ""
        echo "🎯 Grafana 可访问，但需完成插件安装"
    fi
    
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🌐 访问: http://localhost:3000"
    echo "👤 用户: admin"
    echo "🔑 密码: admin"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "📊 下一步:"
    echo "  1. 浏览器打开 http://localhost:3000"
    echo "  2. 登录后检查插件是否已安装"
    echo "  3. 运行压测: ./clickcannon --config config.yaml"
    echo "  4. 在 Dashboard 中查看指标"
else
    echo "❌ Grafana 启动失败"
    echo "   查看日志: docker compose logs grafana"
fi

echo ""
