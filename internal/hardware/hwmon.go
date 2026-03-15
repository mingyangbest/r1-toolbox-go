package hardware

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type HwmonScanner struct{}

type FanInfo struct {
	Chip  string
	Label string
	RPM   int
}

type TempInfo struct {
	Chip  string
	Label string
	Temp  float64
}

func NewHwmonScanner(basePath string) *HwmonScanner {
	return &HwmonScanner{}
}

func (h *HwmonScanner) ScanFans() []FanInfo {
	cmd := exec.Command("sensors")
	out, err := cmd.Output()
	if err != nil {
		return []FanInfo{}
	}
	
	var fans []FanInfo
	lines := strings.Split(string(out), "\n")
	currentChip := ""
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "-isa-") || strings.Contains(line, "-pci-") {
			currentChip = strings.Split(line, "-")[0]
		}
		if strings.Contains(line, "fan") && strings.Contains(line, "RPM") {
			re := regexp.MustCompile(`(.+?):\s+(\d+)\s+RPM`)
			match := re.FindStringSubmatch(line)
			if len(match) >= 3 {
				rpm, _ := strconv.Atoi(match[2])
				fans = append(fans, FanInfo{
					Chip:  currentChip,
					Label: strings.TrimSpace(match[1]),
					RPM:   rpm,
				})
			}
		}
	}
	return fans
}

func (h *HwmonScanner) ScanTemps() []TempInfo {
	cmd := exec.Command("sensors")
	out, err := cmd.Output()
	if err != nil {
		return []TempInfo{}
	}
	
	var temps []TempInfo
	lines := strings.Split(string(out), "\n")
	currentChip := ""
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "-isa-") || strings.Contains(line, "-pci-") {
			currentChip = strings.Split(line, "-")[0]
		}
		if strings.Contains(line, "°C") && !strings.Contains(line, "fan") {
			re := regexp.MustCompile(`(.+?):\s+\+?([\d.]+)°C`)
			match := re.FindStringSubmatch(line)
			if len(match) >= 3 {
				temp, _ := strconv.ParseFloat(match[2], 64)
				temps = append(temps, TempInfo{
					Chip:  currentChip,
					Label: strings.TrimSpace(match[1]),
					Temp:  temp,
				})
			}
		}
	}
	return temps
}

func (h *HwmonScanner) ReadTemp(index int) float64 {
	return 0
}

