#!/bin/bash

# 检查配置文件是否正确

echo "检查 DNS 日志配置..."
echo ""

# 检查文件存在
if [ ! -f "profiles/dns_logs_profile.yaml" ]; then
    echo "错误: profiles/dns_logs_profile.yaml 不存在"
    exit 1
fi

# 统计字段数
FIELDS=$(grep -E '^\s+[a-zA-Z_][a-zA-Z0-9_]*:$' profiles/dns_logs_profile.yaml | wc -l)
echo "字段数: $FIELDS"

# 检查必要字段
echo ""
echo "检查关键字段:"
for field in tnow serverAddress clientAddress firstQueryName serverPort clientPort; do
    if grep -q "^  $field:" profiles/dns_logs_profile.yaml; then
        echo "  ✓ $field"
    else
        echo "  ✗ $field (缺失)"
    fi
done

# 检查生成器类型
echo ""
echo "使用的生成器类型:"
grep "type:" profiles/dns_logs_profile.yaml | grep -v "data_type\|clickhouse_type" | sed 's/.*type://' | sort | uniq -c | sort -rn

# 检查配置文件语法
echo ""
echo "检查 YAML 语法..."
if command -v python3 &> /dev/null; then
    python3 -c "import yaml; yaml.safe_load(open('profiles/dns_logs_profile.yaml'))" 2>&1
    if [ $? -eq 0 ]; then
        echo "✓ YAML 语法正确"
    else
        echo "✗ YAML 语法错误"
    fi
else
    echo "⚠ python3 未安装，跳过语法检查"
fi

# 检查 config_dns_logs.yaml
echo ""
echo "检查 config_dns_logs.yaml:"
if grep -q "enable_custom_fields: true" config_dns_logs.yaml; then
    echo "✓ enable_custom_fields: true"
else
    echo "✗ enable_custom_fields 未启用"
fi

if grep -q "profile_config_file:.*dns_logs_profile.yaml" config_dns_logs.yaml; then
    echo "✓ profile_config_file 正确指向 dns_logs_profile.yaml"
else
    echo "✗ profile_config_file 配置不正确"
fi

if grep -q "enabled: true" config_dns_logs.yaml | head -1; then
    echo "✓ generate.enabled: true"
else
    echo "✗ generate 未启用"
fi

echo ""
echo "配置检查完成"
