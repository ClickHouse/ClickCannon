#!/bin/bash

echo "🔧 修复 Grafana 插件兼容性问题"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ "$EUID" -ne 0 ]; then 
    echo "⚠️  请使用 sudo 运行"
    exit 1
fi

cd /home/sunchanglong/go_project/src/ClickCannon

echo "1️⃣  检查当前 Grafana 版本..."
GRAFANA_VERSION=$(docker exec clickcannon-grafana grafana-server --version 2>/dev/null | grep -oP 'Version \K[0-9.]+' || echo "unknown")
echo "   Grafana 版本: $GRAFANA_VERSION"
echo ""

echo "问题分析："
echo "   插件错误: (0 , r.getAppEvents) is not a function"
echo "   原因: 插件版本太新，与当前 Grafana 版本不兼容"
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "解决方案（3选1）"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

echo "方案1：升级 Grafana 到最新版本（推荐）"
echo "───────────────────────────────────────────"
echo "修改 docker-compose.yml:"
echo "  image: grafana/grafana:latest"
echo "改为:"
echo "  image: grafana/grafana:11.4.0  # 或更新版本"
echo ""
echo "然后:"
echo "  sudo docker compose down"
echo "  sudo docker compose pull"
echo "  sudo docker compose up -d"
echo "  sudo ./离线安装插件.sh install"
echo ""

echo "方案2：使用兼容的旧版本插件（最简单）"
echo "───────────────────────────────────────────"
echo "下载兼容 Grafana 9.x/10.x 的插件版本:"
echo "  v3.x 系列 - 适配 Grafana 9.x"
echo "  v4.0-4.10 - 适配 Grafana 10.x"
echo "  v4.11+ - 需要 Grafana 11.x"
echo ""
echo "推荐使用 v4.8.2（稳定版，兼容 Grafana 10.x）:"
echo "  wget https://github.com/grafana/clickhouse-datasource/releases/download/v4.8.2/grafana-clickhouse-datasource-4.8.2.linux_amd64.zip"
echo "  sudo ./离线安装插件.sh install"
echo ""

echo "方案3：使用 Altinity 插件（替代方案）"
echo "───────────────────────────────────────────"
echo "Altinity 插件更容易安装，兼容性更好:"
echo "  sudo docker exec clickcannon-grafana grafana cli plugins install vertamedia-clickhouse-datasource"
echo "  sudo docker compose restart grafana"
echo ""
echo "注意: 使用 Altinity 需要修改数据源配置"
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "💡 我的推荐"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "如果不想动 Grafana 版本，使用方案2（旧版插件）："
echo ""
echo "  # 1. 删除当前插件"
echo "  sudo docker exec clickcannon-grafana rm -rf /var/lib/grafana/plugins/grafana-clickhouse-datasource"
echo ""
echo "  # 2. 下载兼容版本"
echo "  cd /home/sunchanglong/go_project/src/ClickCannon"
echo "  wget https://github.com/grafana/clickhouse-datasource/releases/download/v4.8.2/grafana-clickhouse-datasource-4.8.2.linux_amd64.zip"
echo ""
echo "  # 3. 重新安装"
echo "  sudo ./离线安装插件.sh install"
echo ""

read -p "是否自动下载兼容版本插件 (v4.8.2)? [y/N] " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo "📥 下载兼容版本插件 v4.8.2..."
    
    cd /home/sunchanglong/go_project/src/ClickCannon
    
    # 删除旧插件
    rm -f grafana-clickhouse-datasource-*.zip
    
    # 下载
    if wget https://github.com/grafana/clickhouse-datasource/releases/download/v4.8.2/grafana-clickhouse-datasource-4.8.2.linux_amd64.zip; then
        echo ""
        echo "✅ 下载成功！"
        echo ""
        echo "现在安装..."
        
        # 删除容器中的旧插件
        docker exec clickcannon-grafana rm -rf /var/lib/grafana/plugins/grafana-clickhouse-datasource
        
        # 安装新插件
        ./离线安装插件.sh install
    else
        echo ""
        echo "❌ 下载失败（网络问题）"
        echo ""
        echo "请手动下载并安装:"
        echo "  https://github.com/grafana/clickhouse-datasource/releases/download/v4.8.2/grafana-clickhouse-datasource-4.8.2.linux_amd64.zip"
    fi
else
    echo ""
    echo "请选择上述方案之一手动操作。"
fi
