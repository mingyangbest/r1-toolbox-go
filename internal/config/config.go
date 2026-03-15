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

func (c *Config) Save() error {
	configPath := getConfigPath()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func DefaultConfig() *Config {
	return &Config{
		Screen: ScreenConfig{Width: 376, Height: 960, Brightness: 36000, Timeout: 300, Background: "", TextColor: "#ffffff"},
		RGB:    RGBConfig{Mode: "solid", Color: "green", I2CBus: "0"},
		Fan: FanConfig{
			CPU: FanCurve{Mode: "auto", PWMIndex: 3, TempSensor: "it8628:temp1", MinTemp: 35, MaxTemp: 75, MinSpeed: 50, MaxSpeed: 255},
			HDD: FanCurve{Mode: "auto", PWMIndex: 2, TempSensor: "it8628:temp2", MinTemp: 30, MaxTemp: 45, MinSpeed: 50, MaxSpeed: 255},
		},
		Web: WebConfig{Enabled: true, Port: 8080},
		Hardware: HardwareConfig{
			BrightnessPath: "/sys/devices/pci0000:00/0000:00:02.0/drm/card0/card0-DSI-1/intel_backlight/brightness",
			HwmonPath:      "/sys/class/hwmon/hwmon3",
			Framebuffer:    "/dev/fb0",
		},
	}
}

