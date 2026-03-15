package monitor

import (
	"fmt"
	netLib "net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type SystemMonitor struct {
	CPUPercent    float64
	CPUCores      []float64
	CPUTemp       float64
	CPUHistory    []float64
	MemPercent    float64
	MemUsedGB     float64
	MemTotalGB    float64
	MemAvailGB    float64
	MemHistory    []float64
	SwapPercent   float64
	NetSendMBps   float64
	NetRecvMBps   float64
	NetHistory    [][2]float64
	LocalIP       string
	Processes     []ProcessInfo
	Disk          *DiskMonitor
	lastNetIO     *net.IOCountersStat
	lastNetTime   time.Time
}

func NewSystemMonitor() *SystemMonitor {
	return &SystemMonitor{
		CPUHistory: make([]float64, 0, 60),
		MemHistory: make([]float64, 0, 60),
		NetHistory: make([][2]float64, 0, 100),
		Disk:       NewDiskMonitor(),
		LocalIP:    "192.168.1.1",
	}
}

func (s *SystemMonitor) UpdateCPU() error {
	percent, _ := cpu.Percent(0, false)
	if len(percent) > 0 {
		s.CPUPercent = percent[0]
		s.CPUHistory = append(s.CPUHistory, percent[0])
		if len(s.CPUHistory) > 60 {
			s.CPUHistory = s.CPUHistory[1:]
		}
	}
	
	cores, _ := cpu.Percent(0, true)
	s.CPUCores = cores
	
	// Go 1.19 的 gopsutil 没有 Temperature()，使用文件读取
	s.CPUTemp = s.getCPUTempFromFile()
	return nil
}

func (s *SystemMonitor) getCPUTempFromFile() float64 {
	// 智能查找coretemp设备
	for i := 0; i <= 10; i++ {
		hwmonPath := fmt.Sprintf("/sys/class/hwmon/hwmon%d", i)
		namePath := hwmonPath + "/name"
		
		// 检查是否是coretemp
		if nameData, err := os.ReadFile(namePath); err == nil {
			if strings.TrimSpace(string(nameData)) == "coretemp" {
				// 找到coretemp，读取Package id 0温度 (temp1_input)
				tempPath := hwmonPath + "/temp1_input"
				if data, err := os.ReadFile(tempPath); err == nil {
					if temp, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
						return float64(temp) / 1000.0
					}
				}
			}
		}
	}
	
	// 备用：thermal_zone
	thermalPaths := []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/class/thermal/thermal_zone1/temp",
	}
	for _, path := range thermalPaths {
		data, err := os.ReadFile(path)
		if err == nil {
			if temp, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return float64(temp) / 1000.0
			}
		}
	}
	return 0
}

func (s *SystemMonitor) UpdateMemory() error {
	v, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	s.MemPercent = v.UsedPercent
	s.MemUsedGB = float64(v.Used) / (1024 * 1024 * 1024)
	s.MemTotalGB = float64(v.Total) / (1024 * 1024 * 1024)
	s.MemAvailGB = float64(v.Available) / (1024 * 1024 * 1024)
	
	s.MemHistory = append(s.MemHistory, v.UsedPercent)
	if len(s.MemHistory) > 60 {
		s.MemHistory = s.MemHistory[1:]
	}
	
	swap, _ := mem.SwapMemory()
	s.SwapPercent = swap.UsedPercent
	return nil
}

func (s *SystemMonitor) UpdateNetwork() error {
	counters, err := net.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return err
	}
	
	now := time.Now()
	if s.lastNetIO != nil {
		elapsed := now.Sub(s.lastNetTime).Seconds()
		s.NetSendMBps = float64(counters[0].BytesSent-s.lastNetIO.BytesSent) / elapsed / (1024 * 1024)
		s.NetRecvMBps = float64(counters[0].BytesRecv-s.lastNetIO.BytesRecv) / elapsed / (1024 * 1024)
		
		s.NetHistory = append(s.NetHistory, [2]float64{s.NetSendMBps, s.NetRecvMBps})
		if len(s.NetHistory) > 100 {
			s.NetHistory = s.NetHistory[1:]
		}
	}
	
	s.lastNetIO = &counters[0]
	s.lastNetTime = now
	s.LocalIP = s.getLocalIPs()
	return nil
}

func (s *SystemMonitor) UpdateProcesses() {
	s.Processes = GetTopProcesses(10)
}

func (s *SystemMonitor) UpdateDisk() error {
	return s.Disk.Update()
}

func (s *SystemMonitor) getLocalIPs() string {
	addrs, err := netLib.InterfaceAddrs()
	if err != nil {
		return "N/A"
	}
	
	var ips []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*netLib.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	
	if len(ips) == 0 {
		return "N/A"
	}
	return strings.Join(ips, ", ")
}
