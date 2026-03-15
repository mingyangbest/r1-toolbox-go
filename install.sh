#!/bin/bash
set -e

echo "=== R1 Toolbox 安装 (Debian) ==="
echo ""

# 检查 GRUB 配置
echo "检查 GRUB 启动参数..."
if [ -f /etc/default/grub ]; then
    cmdline_default=$(grep "^GRUB_CMDLINE_LINUX_DEFAULT=" /etc/default/grub | cut -d'"' -f2)
    cmdline=$(grep "^GRUB_CMDLINE_LINUX=" /etc/default/grub | grep -v "DEFAULT" | cut -d'"' -f2)
    
    echo "GRUB_CMDLINE_LINUX_DEFAULT: $cmdline_default"
    echo "GRUB_CMDLINE_LINUX: $cmdline"
    echo ""
    
    need_fix=0
    if echo "$cmdline_default" | grep -q "video=DSI-1:d\|i915.modeset=0\|nomodeset"; then
        echo "⚠️  CMDLINE_DEFAULT 检测到禁用屏幕的参数"
        need_fix=1
    fi
    if echo "$cmdline" | grep -q "video=DSI-1:d\|i915.modeset=0\|nomodeset"; then
        echo "⚠️  CMDLINE 检测到禁用屏幕的参数"
        need_fix=1
    fi
    
    if [ $need_fix -eq 1 ]; then
        echo ""
        read -p "是否移除这些参数以启用屏幕？(y/n) [y]: " fix_grub
        fix_grub=${fix_grub:-y}
        
        if [ "$fix_grub" = "y" ]; then
            echo "修改 GRUB 配置..."
            new_cmdline_default=$(echo "$cmdline_default" | sed 's/video=DSI-1:d//g' | sed 's/i915.modeset=0//g' | sed 's/nomodeset//g' | sed 's/  */ /g' | sed 's/^ *//;s/ *$//')
            new_cmdline=$(echo "$cmdline" | sed 's/video=DSI-1:d//g' | sed 's/i915.modeset=0//g' | sed 's/nomodeset//g' | sed 's/  */ /g' | sed 's/^ *//;s/ *$//')
            
            sudo sed -i "s/^GRUB_CMDLINE_LINUX_DEFAULT=.*/GRUB_CMDLINE_LINUX_DEFAULT=\"$new_cmdline_default\"/" /etc/default/grub
            if [ -n "$cmdline" ]; then
                sudo sed -i "s/^GRUB_CMDLINE_LINUX=.*/GRUB_CMDLINE_LINUX=\"$new_cmdline\"/" /etc/default/grub
            fi
            
            sudo update-grub
            echo "✓ GRUB 已更新，重启后生效"
            echo ""
            read -p "是否现在重启？(y/n) [n]: " do_reboot
            if [ "$do_reboot" = "y" ]; then
                sudo reboot
                exit 0
            fi
        fi
    else
        echo "✓ 未检测到禁用屏幕的参数"
    fi
    echo ""
fi

echo "提示: 如果在中国大陆遇到 GitHub 访问问题，建议配置代理"
echo ""

read -p "是否配置代理？(y/n) [n]: " use_proxy
use_proxy=${use_proxy:-n}

if [ "$use_proxy" = "y" ]; then
    read -p "代理地址 (如 http://127.0.0.1:7890): " proxy_url
    if [ -n "$proxy_url" ]; then
        export HTTP_PROXY="$proxy_url"
        export HTTPS_PROXY="$proxy_url"
        export http_proxy="$proxy_url"
        export https_proxy="$proxy_url"
        echo "代理已设置: $proxy_url"
    fi
fi

read -p "使用 Go 模块国内镜像？(y/n) [y]: " use_goproxy
use_goproxy=${use_goproxy:-y}

if [ "$use_goproxy" = "y" ]; then
    export GOPROXY=https://goproxy.cn,direct
    echo "Go 模块镜像已设置: goproxy.cn"
fi

# 检查并安装依赖
echo ""
echo "检查系统依赖..."

if ! command -v go &> /dev/null; then
    echo "安装 Go 编译环境..."
    sudo apt update
    sudo apt install -y golang-go
fi

if ! dpkg -l | grep -q libsdl2-dev; then
    echo "安装 SDL2 开发库..."
    sudo apt install -y libsdl2-dev libsdl2-ttf-dev
fi

echo "安装中文字体..."
sudo apt install -y fonts-wqy-zenhei

if ! command -v sensors &> /dev/null; then
    echo "安装 lm-sensors..."
    sudo apt install -y lm-sensors
fi

if ! command -v smartctl &> /dev/null; then
    echo "安装 smartmontools..."
    sudo apt install -y smartmontools
fi

if ! command -v i2cset &> /dev/null; then
    echo "安装 i2c-tools..."
    sudo apt install -y i2c-tools
fi

echo ""
read -p "Web 端口 [8080]: " web_port
web_port=${web_port:-8080}

CONFIG_DIR="/etc/r1-control"
CONFIG_FILE="$CONFIG_DIR/config.yaml"

echo ""
echo "下载 Go 模块依赖..."
go mod tidy
go mod download

echo "编译中..."
make build

echo "安装二进制文件..."
sudo mkdir -p "$CONFIG_DIR"
sudo cp build/r1-toolbox /usr/local/bin/
sudo chmod +x /usr/local/bin/r1-toolbox

sudo tee "$CONFIG_FILE" > /dev/null <<EOF
screen:
  width: 376
  height: 960
  brightness: 36000
  timeout: 300

rgb:
  mode: solid
  color: green
  i2c_bus: "0"

fan:
  cpu:
    mode: auto
    pwm_index: 3
    temp_sensor: "it8628:temp1"
    min_temp: 35
    max_temp: 75
    min_speed: 50
    max_speed: 255
  hdd:
    mode: auto
    pwm_index: 2
    temp_sensor: "it8628:temp2"
    min_temp: 30
    max_temp: 45
    min_speed: 50
    max_speed: 255

web:
  enabled: true
  port: $web_port

hardware:
  brightness_path: /sys/devices/pci0000:00/0000:00:02.0/drm/card0/card0-DSI-1/intel_backlight/brightness
  hwmon_path: /sys/class/hwmon/hwmon3
  framebuffer: /dev/fb0
EOF

sudo tee /etc/systemd/system/r1-toolbox.service > /dev/null <<EOF
[Unit]
Description=R1 Toolbox Display Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/r1-toolbox
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable r1-toolbox
sudo systemctl start r1-toolbox

echo ""
echo "✓ 安装完成！"
echo "Web 界面: http://localhost:$web_port"
echo "在网页中配置屏幕亮度、RGB 灯效、风扇等参数"
echo "查看状态: sudo systemctl status r1-toolbox"
