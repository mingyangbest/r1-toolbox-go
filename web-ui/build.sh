#!/bin/bash
set -e

cd "$(dirname "$0")"

echo "安装依赖..."
npm install

echo "构建 Web UI..."
npm run build

echo "生成 Go 嵌入文件..."
cd ../internal/web/dist
cat index.html | sed 's/"/\\"/g' | sed 's/$/\\n/' > ../embed.go.tmp

cat > ../embed.go << 'EOF'
package web

const htmlTemplate = `
EOF

cat ../embed.go.tmp >> ../embed.go
echo '`' >> ../embed.go

rm ../embed.go.tmp

echo "✓ Web UI 构建完成"
