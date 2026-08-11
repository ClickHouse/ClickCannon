#!/bin/bash

echo "📦 Grafana ClickHouse 插件 - 离线安装指南"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# 插件最新版本
PLUGIN_VERSION="4.20.0"
PLUGIN_URL="https://github.com/grafana/clickhouse-datasource/releases/download/v${PLUGIN_VERSION}/grafana-clickhouse-datasource-${PLUGIN_VERSION}.linux_amd64.zip"

echo "步骤说明："
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "1️⃣  下载插件（在有网络的机器上）"
echo "   wget ${PLUGIN_URL}"
echo "   或访问: https://grafana.com/grafana/plugins/grafana-clickhouse-datasource/"
echo ""
echo "2️⃣  传输到目标服务器"
echo "   scp grafana-clickhouse-datasource-${PLUGIN_VERSION}.linux_amd64.zip user@server:/path/"
echo ""
echo "3️⃣  解压并安装（本机执行）"
echo "   运行此脚本: sudo ./离线安装插件.sh install"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ "$1" == "install" ]; then
    if [ "$EUID" -ne 0 ]; then 
        echo "⚠️  请使用 sudo 运行"
        exit 1
    fi
    
    cd /home/sunchanglong/go_project/src/ClickCannon
    
    # 查找插件文件
    PLUGIN_FILE=$(ls grafana-clickhouse-datasource-*.linux_amd64.zip 2>/dev/null | head -1)
    
    if [ -z "$PLUGIN_FILE" ]; then
        echo ""
        echo "❌ 未找到插件文件！"
        echo ""
        echo "请下载插件文件到当前目录："
        echo "   cd /home/sunchanglong/go_project/src/ClickCannon"
        echo "   wget ${PLUGIN_URL}"
        echo ""
        echo "或从以下地址手动下载："
        echo "   https://github.com/grafana/clickhouse-datasource/releases/latest"
        exit 1
    fi
    
    echo ""
    echo "✅ 找到插件文件: $PLUGIN_FILE"
    echo ""
    
    # 创建临时目录
    echo "1️⃣  解压插件..."
    mkdir -p /tmp/grafana-plugins
    unzip -q "$PLUGIN_FILE" -d /tmp/grafana-plugins/
    
    if [ $? -ne 0 ]; then
        echo "❌ 解压失败"
        exit 1
    fi
    
    echo "   ✅ 解压成功"
    echo ""
    
    # 确保 Grafana 正在运行
    echo "2️⃣  检查 Grafana 容器..."
    if ! docker ps | grep -q clickcannon-grafana; then
        echo "   启动 Grafana..."
        docker compose up -d
        sleep 15
    fi
    echo "   ✅ Grafana 运行中"
    echo ""
    
    # 复制插件到容器
    echo "3️⃣  复制插件到 Grafana 容器..."
    docker cp /tmp/grafana-plugins/grafana-clickhouse-datasource clickcannon-grafana:/var/lib/grafana/plugins/
    
    if [ $? -ne 0 ]; then
        echo "❌ 复制失败"
        exit 1
    fi
    
    echo "   ✅ 复制成功"
    echo ""
    
    # 设置权限
    echo "4️⃣  设置权限..."
    docker exec clickcannon-grafana chown -R grafana:grafana /var/lib/grafana/plugins/grafana-clickhouse-datasource
    echo "   ✅ 权限已设置"
    echo ""
    
    # 重启容器
    echo "5️⃣  重启 Grafana..."
    docker compose restart grafana
    sleep 15
    echo "   ✅ 重启完成"
    echo ""
    
    # 验证
    echo "6️⃣  验证安装..."
    if docker exec clickcannon-grafana grafana cli plugins ls | grep -q "grafana-clickhouse-datasource"; then
        echo "   ✅ 插件已安装！"
        
        VERSION=$(docker exec clickcannon-grafana grafana cli plugins ls | grep grafana-clickhouse-datasource | awk '{print $NF}')
        echo "   版本: $VERSION"
    else
        echo "   ⚠️  插件可能未正确安装"
    fi
    
    # 清理
    echo ""
    echo "7️⃣  清理临时文件..."
    rm -rf /tmp/grafana-plugins
    echo "   ✅ 清理完成"
    echo ""
    
    # 检查 Grafana 可访问性
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    if curl -s http://localhost:3000 > /dev/null 2>&1; then
        echo "🎉 安装完成！"
        echo ""
        echo "🌐 访问: http://localhost:3000"
        echo "👤 用户: admin"
        echo "🔑 密码: admin"
        echo ""
        echo "✅ ClickHouse 数据源已自动配置"
        echo "✅ Dashboard 已自动导入"
        echo ""
        echo "📊 下一步:"
        echo "  1. 打开浏览器访问 http://localhost:3000"
        echo "  2. 登录后前往 Dashboards → ClickCannon"
        echo "  3. 在另一终端运行: ./clickcannon --config config.yaml"
        echo "  4. 记录 run_id，在 Dashboard 中选择查看"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    else
        echo "⚠️  Grafana 可能未正常启动"
        echo "   查看日志: docker compose logs grafana"
    fi
    
elif [ "$1" == "download" ]; then
    echo ""
    echo "📥 开始下载插件..."
    echo ""
    
    cd /home/sunchanglong/go_project/src/ClickCannon
    
    if wget --version > /dev/null 2>&1; then
        wget "${PLUGIN_URL}"
    elif curl --version > /dev/null 2>&1; then
        curl -L -O "${PLUGIN_URL}"
    else
        echo "❌ 未找到 wget 或 curl"
        echo ""
        echo "请手动下载:"
        echo "   ${PLUGIN_URL}"
        echo ""
        echo "或访问:"
        echo "   https://github.com/grafana/clickhouse-datasource/releases/latest"
        exit 1
    fi
    
    if [ $? -eq 0 ]; then
        echo ""
        echo "✅ 下载成功！"
        echo ""
        echo "现在运行: sudo ./离线安装插件.sh install"
    else
        echo ""
        echo "❌ 下载失败"
        echo ""
        echo "请手动下载并放到当前目录:"
        echo "   ${PLUGIN_URL}"
    fi
    
else
    echo ""
    echo "用法:"
    echo "  ./离线安装插件.sh download    # 下载插件（如果有网络）"
    echo "  ./离线安装插件.sh install    # 安装插件（需要插件文件在当前目录）"
    echo ""
fi
