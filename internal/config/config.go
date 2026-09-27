package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	ScreenWidth  = 376
	ScreenHeight = 960
	FrameRate    = 25
)

func getConfigPath() string {
	// 优先使用飞牛NAS环境变量
	if pkgEtc := os.Getenv("TRIM_PKGETC"); pkgEtc != "" {
		return filepath.Join(pkgEtc, "config.yaml")
	}
	// 备用路径
	return "/etc/r1-toolbox/config.yaml"
}

type Config struct {
	Screen   ScreenConfig   `yaml:"screen"`
	RGB      RGBConfig      `yaml:"rgb"`
	Fan      FanConfig      `yaml:"fan"`
	Web      WebConfig      `yaml:"web"`
	Hardware HardwareConfig `yaml:"hardware"`
}

type ScreenConfig struct {
	Width      int    `yaml:"width"`
	Height     int    `yaml:"height"`
	Brightness int    `yaml:"brightness"`
	Timeout    int    `yaml:"timeout"`
	Background string `yaml:"background"`
	TextColor  string `yaml:"text_color"`
}

type RGBConfig struct {
	Mode   string `yaml:"mode"`
	Color  string `yaml:"color"`
	I2CBus string `yaml:"i2c_bus"`
}

type FanConfig struct {
	CPU FanCurve `yaml:"cpu"`
	HDD FanCurve `yaml:"hdd"`
}

type FanCurve struct {
	Mode       string `yaml:"mode"`
	PWMIndex   int    `yaml:"pwm_index"`
	TempSensor string `yaml:"temp_sensor"`
	MinTemp    int    `yaml:"min_temp"`
	MaxTemp    int    `yaml:"max_temp"`
	MinSpeed   int    `yaml:"min_speed"`
	MaxSpeed   int    `yaml:"max_speed"`
}

type WebConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
	// Token 面板访问令牌。为空时 main 启动会自动生成并回写到这里；
	// 所有 /api/* 接口要求请求携带该令牌（X-Token 头或 token 查询参数）。
	// 令牌同时打印到启动日志并显示在屏幕「系统」页 —— 物理接触设备即授权。
	Token string `yaml:"token"`
}

type HardwareConfig struct {
	BrightnessPath string `yaml:"brightness_path"`
	HwmonPath      string `yaml:"hwmon_path"`
	Framebuffer    string `yaml:"framebuffer"`
}

func Load() (*Config, error) {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return DefaultConfig(), nil
	}
	
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save 把当前配置写回磁盘。
//
// G2 修复（2026-09-27）：旧实现 yaml.Marshal 整文件覆盖，用户手写的
// 注释全部丢失。新实现把新值合并进旧文件的语法树（yaml.Node）：
// 键序、行注释、引号风格原样保留，仅标量值被更新；新版本新增的
// 字段会被追加到对应小节末尾（头注释随新字段一起带入）。
//
// 三重兜底，任何一步不满足都退回旧的整文件写出（宁丢注释不丢数据）：
//   - 旧文件读不到（首次写）→ 整文件写出；
//   - 旧文件不是合法 YAML 映射文档（损坏）→ 整文件写出；
//   - 合并结果序列化失败 → 整文件写出。
func (c *Config) Save() error {
	configPath := getConfigPath()

	fresh, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	old, err := os.ReadFile(configPath)
	if err != nil || len(old) == 0 {
		return os.WriteFile(configPath, fresh, 0644)
	}

	var dst yaml.Node
	if err := yaml.Unmarshal(old, &dst); err != nil ||
		dst.Kind != yaml.DocumentNode || len(dst.Content) == 0 ||
		dst.Content[0].Kind != yaml.MappingNode {
		return os.WriteFile(configPath, fresh, 0644)
	}

	var src yaml.Node
	if err := yaml.Unmarshal(fresh, &src); err != nil ||
		len(src.Content) == 0 || src.Content[0].Kind != yaml.MappingNode {
		return os.WriteFile(configPath, fresh, 0644)
	}

	mergeMappingNode(src.Content[0], dst.Content[0])

	out, err := yaml.Marshal(&dst)
	if err != nil {
		return os.WriteFile(configPath, fresh, 0644)
	}
	return os.WriteFile(configPath, out, 0644)
}

// mergeMappingNode 把 src 映射里的值合并进 dst 映射（保留 dst 的键序、
// 注释与标量样式）。
//   - dst 已有的键：值用 src 覆盖；标量保留 dst 的 Style（用户写的
//     引号风格不丢），非标量（嵌套映射/序列）整体替换；
//   - dst 缺失的键：连同键值对整体追加（新字段的注释随 src 带入）；
//   - 双方对应位置都是映射时递归合并（fan.cpu/hdd 等小节）。
func mergeMappingNode(src, dst *yaml.Node) {
	dstVal := map[string]*yaml.Node{}
	for i := 0; i+1 < len(dst.Content); i += 2 {
		dstVal[dst.Content[i].Value] = dst.Content[i+1]
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		sk, sv := src.Content[i], src.Content[i+1]
		dv, ok := dstVal[sk.Value]
		if !ok {
			nk, nv := *sk, *sv
			dst.Content = append(dst.Content, &nk, &nv)
			continue
		}
		if sv.Kind == yaml.MappingNode && dv.Kind == yaml.MappingNode {
			mergeMappingNode(sv, dv)
			continue
		}
		if sv.Kind == yaml.ScalarNode && dv.Kind == yaml.ScalarNode {
			dv.Value = sv.Value
			dv.Tag = sv.Tag
			// dv.Style 保留：不强改为 src 的风格
			continue
		}
		*dv = *sv
	}
}

func DefaultConfig() *Config {
	return &Config{
		Screen: ScreenConfig{Width: 376, Height: 960, Brightness: 36000, Timeout: 300, Background: "", TextColor: "#ffffff"},
		RGB:    RGBConfig{Mode: "solid", Color: "green", I2CBus: "0"},
		Fan: FanConfig{
			CPU: FanCurve{Mode: "auto", PWMIndex: 3, TempSensor: "it8628:temp1", MinTemp: 35, MaxTemp: 75, MinSpeed: 50, MaxSpeed: 255},
			HDD: FanCurve{Mode: "auto", PWMIndex: 2, TempSensor: "it8628:temp2", MinTemp: 40, MaxTemp: 60, MinSpeed: 50, MaxSpeed: 255},
		},
		Web: WebConfig{Enabled: true, Port: 8080},
		Hardware: HardwareConfig{
			BrightnessPath: "/sys/devices/pci0000:00/0000:00:02.0/drm/card0/card0-DSI-1/intel_backlight/brightness",
			HwmonPath:      "/sys/class/hwmon/hwmon3",
			Framebuffer:    "/dev/fb0",
		},
	}
}

