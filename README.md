# R1 Toolbox

海康威视 R1/R1X NAS 设备的硬件控制工具，提供触摸屏显示、RGB灯控制、风扇管理和Web控制面板。

## 功能特性

- 📺 触摸屏显示：系统信息、网络状态、硬件监控
- 🌈 RGB氛围灯控制：多种灯效模式
- 🌡️ 智能风扇控制：CPU/机箱风扇自动调速
- 🌐 Web控制面板：远程管理和配置
- 🖼️ 自定义背景：支持上传和裁剪背景图片

## 系统要求

- 海康威视 R1 或 R1X 设备
- Debian/Ubuntu 系统
- Go 1.19+ (编译时)
- SDL2 开发库

## 安装

```bash
# 克隆仓库
git clone <repository-url>
cd r1-toolbox-go

# 运行安装脚本
sudo ./install.sh
```

安装脚本会自动：
- 安装系统依赖（SDL2、lm-sensors、i2c-tools等）
- 编译 Go 程序
- 创建配置文件
- 配置 systemd 服务

## 使用

```bash
# 启动服务
sudo systemctl start r1-toolbox

# 停止服务
sudo systemctl stop r1-toolbox

# 查看状态
sudo systemctl status r1-toolbox

# 查看日志
sudo journalctl -u r1-toolbox -f
```

访问 Web 界面：`http://<设备IP>:<port>`

## 配置

配置文件位于 `/etc/r1-toolbox/config.yaml`，可配置：
- 屏幕分辨率和亮度
- RGB灯模式和颜色
- 风扇控制策略
- Web服务端口

## 开发

```bash
# 安装依赖
go mod download

# 编译
make build

# 本地运行
./build/r1-toolbox
```


## 许可证

MIT License
