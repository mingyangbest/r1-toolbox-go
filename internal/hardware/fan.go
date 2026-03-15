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
		HDDMinTemp:    30,
		HDDMaxTemp:    45,
		cpuTempSensor: "coretemp:Package id 0",
		hddTempSensor: "it8628:temp2",
		lastCPUSpeed:  -1,
		lastHDDSpeed:  -1,
	}
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
		hddTemp := f.GetTemp(f.hddTempSensor)
		
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
