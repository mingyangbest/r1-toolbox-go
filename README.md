# R1 Toolbox (Enhanced Fork)

海康威视 R1 / R1X NAS 前置触摸屏的硬件管理工具：五页信息面板（概览/性能/硬盘/网络/系统）、Web 控制面板、智能风扇调速、天气展示。

> 本项目基于 [lemon6515/r1-toolbox-go](https://github.com/lemon6515/r1-toolbox-go) 增强 fork 而来，遵循上游 **GPL-3.0 + Commons Clause** 许可证，感谢上游作者的开源工作。

## 本 fork 的主要改进

| 方向 | 内容 |
|---|---|
| **性能** | CPU 占用从约 89% 优化到 **单核 2.1%**（跳帧渲染 + 局部重绘 + 圆角遮罩缓存 + 亚像素插值 + 采样减频） |
| **安全** | Web 面板令牌鉴权（首次启动自动生成、屏幕系统页显示）、亮度/风扇曲线等接口输入校验、修复磁盘监控数据竞争（`-race` 验证） |
| **体验** | 天气图标静态化（相位冻结）、配置文件保存保留注释、崩溃自愈（systemd 加固） |
| **代码** | 渲染器模块化拆分（canvas / textrender / theme / widgets）、删除无人消费的死代码 |

## 硬件与系统要求

- 海康威视 **R1** 或 **R1X** NAS（程序直接操作前置屏幕的帧缓冲 `/dev/fb0`、背光 sysfs、风扇 hwmon，**无法在普通电脑上运行**）
- 飞牛 fnOS 或 Debian/Ubuntu 系发行版
- 编译需要 Go 1.19+；Docker 构建方式无需在本机装 Go

## 部署方式

### 方式 A：一键脚本（推荐，宿主机 systemd 服务）

```bash
git clone https://github.com/<YOUR_USERNAME>/r1-toolbox-go.git
cd r1-toolbox-go
sudo ./install.sh
```

脚本自动完成：系统依赖安装（SDL2、smartmontools 等）、编译、配置文件生成、systemd 服务注册。

```bash
sudo systemctl start r1-toolbox    # 启动（开机自启默认开启）
sudo systemctl status r1-toolbox   # 查看状态
sudo journalctl -u r1-toolbox -f   # 看日志
```

### 方式 B：Docker 编译（本机没有 Go 环境时）

```bash
# 用 Docker 编译出二进制，产物落在 ./build/r1-toolbox
docker compose run --rm build

# 然后用仓库里的 install.sh 部署，或手动复制：
sudo cp build/r1-toolbox /usr/local/bin/
```

### 方式 C：Docker 容器运行（⚠️ 实验性）

屏幕渲染需要特权设备访问，容器方式仅作尝试，**不保证在所有固件上可用**：

```bash
docker compose up -d screen   # privileged + /dev/fb0 + host 网络
```

遇到问题请回退方式 A。日常使用**推荐方式 A**。

### 方式 D：fpk 应用包（飞牛应用中心形态）

用飞牛官方 fnpack 工具打包为 `.fpk` 手动安装（本仓库暂未附带打包配置，走方式 A 即可）。

## Web 控制面板

启动后浏览器访问 `http://<设备IP>:8080`。所有控制接口需要令牌：

- 首次启动自动生成 8 位令牌，打印在启动日志中（`journalctl -u r1-toolbox | grep 令牌`），同时显示在屏幕「系统」页底部
- 浏览器首次访问时按提示输入一次即可（自动记住）

## 配置

配置文件：`/etc/r1-toolbox/config.yaml`（程序每 5 秒热重载，改完即生效）。字段说明见 [`config.example.yaml`](config.example.yaml)。

天气数据来自[心知天气](https://www.seniverse.com/)免费接口，API Key 继承自上游项目（已随上游公开）；如失效可自行注册并替换 `internal/netinfo/netinfo.go` 中的 `senKey`。

## 已知限制

- 侧边 RGB 灯条：上游代码保留了 RGB 模块，但在 R1 前屏硬件上**未发现系统级软件控制通路**（实测内核 LED / I2C / ACPI 均无接口），灯效可能由屏体 MCU 硬件自带
- 本程序为非官方项目，与海康威视、飞牛官方均无关联；刷写/安装产生的风险自负

## 开发

```bash
make build          # 或 docker compose run --rm build
go vet ./...
```

## 许可证

本项目继承上游的 **GPL-3.0 + Commons Clause** 许可证（见 [LICENSE](LICENSE)）：

- 任何分发/修改须同样以 GPL-3.0 开源，并保留原版权声明
- **Commons Clause**：不得将本软件及其衍生作品**用于销售或其他商业目的**

© 上游原作者 [lemon6515](https://github.com/lemon6515/r1-toolbox-go)；本 fork 的改动部分同样以上述许可证发布。
