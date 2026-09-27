package hardware

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type FanController struct {
	it8628Path    string
	coretempPath  string
	cpuPWMIndex   int
	hddPWMIndex   int
	cpuMode       string
	hddMode       string
	CPUMinTemp    int
	CPUMaxTemp    int
	HDDMinTemp    int
	HDDMaxTemp    int
	cpuTempSensor string
	hddTempSensor string
	lastCPUSpeed  int
	lastHDDSpeed  int

	// hddTempSource 由主程序注入：返回「真实硬盘温度」（多盘取最高）。
	//
	// 这是 2026-09-27 洋哥提出的修正。原来机箱风扇读的是 it8628:temp2 ——
	// 那颗传感器焊在主板上（硬盘上方），实测常年在 47~48°C，而硬盘本体
	// 只有 37°C（SATA）/ 50°C（NVMe）。被控量本身偏高 10°C，再配上
	// 30~45°C 的曲线，结果就是**永远顶在 255 满速**，模式切了也听不出差别。
	// 现在改为读盘体温度，且与硬盘页显示的是同一个数据源（monitor.DiskMonitor），
	// 屏幕上的数字和调速用的数字永远一致。
	// 注入为空或读不到时，回退到 hddTempSensor（至少比失控安全）。
	hddTempSource func() float64
}

func NewFanController() *FanController {
	it8628 := DiscoverIT8628Path()
	coretemp := DiscoverCoretempPath()
	
	log.Printf("发现 IT8628: %s", it8628)
	log.Printf("发现 Coretemp: %s", coretemp)
	
	return &FanController{
		it8628Path:    it8628,
		coretempPath:  coretemp,
		cpuPWMIndex:   3,
		hddPWMIndex:   2,
		cpuMode:       "auto",
		hddMode:       "auto",
		CPUMinTemp:    35,
		CPUMaxTemp:    75,
		// 机箱风扇曲线：40°C 起调、60°C 满速。
		// 盘体 37°C → 最低档；50°C（本机 NVMe 常态）→ 约 152/255；60°C → 满速。
		// 想看更安静就把 MaxTemp 调大，更保守就调小（配置项 fan.hdd.min_temp/max_temp）。
		HDDMinTemp:    40,
		HDDMaxTemp:    60,
		cpuTempSensor: "coretemp:Package id 0",
		hddTempSensor: "it8628:temp2", // 仅作兜底：盘温读不到时才用它
		lastCPUSpeed:  -1,
		lastHDDSpeed:  -1,
	}
}

// SetHDDTempSource 注入真实硬盘温度来源（由 main 接 monitor.DiskMonitor）
func (f *FanController) SetHDDTempSource(fn func() float64) { f.hddTempSource = fn }

// hddTemp 取机箱风扇的被控温度：优先盘体温度，取不到才回退主板传感器
func (f *FanController) hddTemp() float64 {
	if f.hddTempSource != nil {
		if v := f.hddTempSource(); v > 0 {
			return v
		}
	}
	return f.GetTemp(f.hddTempSensor)
}

func (f *FanController) Init() error {
	if f.it8628Path == "" {
		return fmt.Errorf("未找到 IT8628 芯片")
	}
	
	log.Printf("设置 PWM%d 为手动模式", f.cpuPWMIndex)
	f.setPWMEnable(f.cpuPWMIndex, 1)
	
	log.Printf("设置 PWM%d 为手动模式", f.hddPWMIndex)
	f.setPWMEnable(f.hddPWMIndex, 1)
	
	return nil
}

func (f *FanController) setPWMEnable(pwmIndex int, mode int) error {
	path := filepath.Join(f.it8628Path, fmt.Sprintf("pwm%d_enable", pwmIndex))
	log.Printf("写入 %s = %d", path, mode)
	return os.WriteFile(path, []byte(strconv.Itoa(mode)), 0644)
}

func (f *FanController) SetCPUMode(mode string) {
	f.cpuMode = mode
	log.Printf("CPU 风扇模式: %s", mode)
}

func (f *FanController) SetHDDMode(mode string) {
	f.hddMode = mode
	log.Printf("HDD 风扇模式: %s", mode)
}

func (f *FanController) GetCPUMode() string {
	return f.cpuMode
}

func (f *FanController) GetHDDMode() string {
	return f.hddMode
}

// GetFanSpeeds 返回 (CPU风扇RPM, 机箱风扇RPM)。
//
// 直接读 IT8628 的 fan*_input 真实转速 —— 界面上的「自动/低速/中速/高速」
// 只是模式名，转速才是可被验证的事实（写 pwmN 之后转速会实际变化）。
// 读不到返回 0，调用方据此决定是否显示。
func (f *FanController) GetFanSpeeds() (int, int) {
	if f.it8628Path == "" {
		return 0, 0
	}
	readRPM := func(idx int) int {
		if idx <= 0 {
			return 0
		}
		b, err := os.ReadFile(filepath.Join(f.it8628Path, fmt.Sprintf("fan%d_input", idx)))
		if err != nil {
			return 0
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || v < 0 {
			return 0
		}
		return v
	}
	return readRPM(f.cpuPWMIndex), readRPM(f.hddPWMIndex)
}

// CurrentPWMDuty 返回当前实际写入的 PWM 占空比 0-255（读硬件，不是读模式名）
func (f *FanController) CurrentPWMDuty() (int, int) {
	read := func(idx int) int {
		if idx <= 0 {
			return -1
		}
		b, err := os.ReadFile(filepath.Join(f.it8628Path, fmt.Sprintf("pwm%d", idx)))
		if err != nil {
			return -1
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return -1
		}
		return v
	}
	return read(f.cpuPWMIndex), read(f.hddPWMIndex)
}

func (f *FanController) SetCPUTempRange(minTemp, maxTemp int) {
	f.CPUMinTemp = minTemp
	f.CPUMaxTemp = maxTemp
}

func (f *FanController) SetHDDTempRange(minTemp, maxTemp int) {
	f.HDDMinTemp = minTemp
	f.HDDMaxTemp = maxTemp
}

func (f *FanController) SetCPUSpeed(speed int) error {
	if speed < 0 {
		speed = 0
	}
	if speed > 255 {
		speed = 255
	}
	
	if f.lastCPUSpeed == speed {
		return nil
	}
	
	path := filepath.Join(f.it8628Path, fmt.Sprintf("pwm%d", f.cpuPWMIndex))
	log.Printf("设置 CPU 风扇速度: %s = %d", path, speed)
	
	err := os.WriteFile(path, []byte(strconv.Itoa(speed)), 0644)
	if err == nil {
		f.lastCPUSpeed = speed
	} else {
		log.Printf("写入失败: %v", err)
	}
	return err
}

func (f *FanController) SetHDDSpeed(speed int) error {
	if speed < 0 {
		speed = 0
	}
	if speed > 255 {
		speed = 255
	}
	
	if f.lastHDDSpeed == speed {
		return nil
	}
	
	path := filepath.Join(f.it8628Path, fmt.Sprintf("pwm%d", f.hddPWMIndex))
	log.Printf("设置 HDD 风扇速度: %s = %d", path, speed)
	
	err := os.WriteFile(path, []byte(strconv.Itoa(speed)), 0644)
	if err == nil {
		f.lastHDDSpeed = speed
	} else {
		log.Printf("写入失败: %v", err)
	}
	return err
}


func (f *FanController) GetTemp(sensor string) float64 {
	if sensor == "" {
		return f.getCoretempPackage()
	}
	
	parts := strings.Split(sensor, ":")
	if len(parts) != 2 {
		return 30.0
	}
	
	chip := parts[0]
	label := parts[1]
	
	if chip == "coretemp" {
		return f.getCoretempByLabel(label)
	} else if chip == "it8628" || chip == "it87" {
		return f.getIT8628TempByLabel(label)
	} else if chip == "nvme" {
		return f.getNVMeTempByLabel(label)
	}
	
	return 30.0
}

func (f *FanController) getCoretempPackage() float64 {
	if f.coretempPath == "" {
		return 30.0
	}
	
	path := filepath.Join(f.coretempPath, "temp1_input")
	data, err := os.ReadFile(path)
	if err != nil {
		return 30.0
	}
	
	temp, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return float64(temp) / 1000.0
}

func (f *FanController) getCoretempByLabel(label string) float64 {
	if f.coretempPath == "" {
		return 30.0
	}
	
	if label == "Package id 0" {
		return f.getCoretempPackage()
	}
	
	for i := 1; i <= 10; i++ {
		labelPath := filepath.Join(f.coretempPath, fmt.Sprintf("temp%d_label", i))
		labelData, err := os.ReadFile(labelPath)
		if err != nil {
			continue
		}
		
		if strings.TrimSpace(string(labelData)) == label {
			inputPath := filepath.Join(f.coretempPath, fmt.Sprintf("temp%d_input", i))
			data, err := os.ReadFile(inputPath)
			if err == nil {
				temp, _ := strconv.Atoi(strings.TrimSpace(string(data)))
				return float64(temp) / 1000.0
			}
		}
	}
	
	return 30.0
}

func (f *FanController) getIT8628TempByLabel(label string) float64 {
	if f.it8628Path == "" {
		return 30.0
	}
	
	tempIndex := 0
	if strings.HasPrefix(label, "temp") {
		fmt.Sscanf(label, "temp%d", &tempIndex)
	}
	
	if tempIndex > 0 {
		path := filepath.Join(f.it8628Path, fmt.Sprintf("temp%d_input", tempIndex))
		data, err := os.ReadFile(path)
		if err == nil {
			temp, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			return float64(temp) / 1000.0
		}
	}
	
	return 30.0
}

func (f *FanController) getNVMeTempByLabel(label string) float64 {
	nvmePath := DiscoverNVMePath()
	if nvmePath == "" {
		return 30.0
	}
	
	path := filepath.Join(nvmePath, "temp1_input")
	data, err := os.ReadFile(path)
	if err == nil {
		temp, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return float64(temp) / 1000.0
	}
	
	return 30.0
}

func (f *FanController) Run() {
	for {
		cpuTemp := f.GetTemp(f.cpuTempSensor)
		// 机箱风扇被控量 = 真实盘体温度（多盘取最高）
		hddTemp := f.hddTemp()

		var cpuSpeed int
		if f.cpuMode == "auto" {
			cpuSpeed = f.CalculateSpeed(cpuTemp, f.CPUMinTemp, f.CPUMaxTemp, 50, 255)
		} else {
			cpuSpeed = map[string]int{"low": 85, "medium": 170, "high": 255}[f.cpuMode]
		}

		var hddSpeed int
		if f.hddMode == "auto" {
			hddSpeed = f.CalculateSpeed(hddTemp, f.HDDMinTemp, f.HDDMaxTemp, 50, 255)
		} else {
			hddSpeed = map[string]int{"low": 85, "medium": 170, "high": 255}[f.hddMode]
		}

		f.SetCPUSpeed(cpuSpeed)
		f.SetHDDSpeed(hddSpeed)

		time.Sleep(time.Second)
	}
}

func (f *FanController) CalculateSpeed(temp float64, minTemp, maxTemp, minSpeed, maxSpeed int) int {
	if temp <= float64(minTemp) {
		return max(minSpeed, 25)
	}
	if temp >= float64(maxTemp) {
		return maxSpeed
	}
	ratio := (temp - float64(minTemp)) / float64(maxTemp-minTemp)
	speed := int(float64(minSpeed) + ratio*float64(maxSpeed-minSpeed))
	return max(max(minSpeed, 25), min(speed, maxSpeed))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
