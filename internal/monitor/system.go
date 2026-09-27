package monitor

import (
	"encoding/json"
	"fmt"
	netLib "net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// SystemMonitor 采集系统实时指标。
//
// 数据真实性约定（每一屏上的数字都必须能追溯到内核/接口的真实读数）：
//   - 运行时长取 /proc/uptime（系统开机至今），不是本进程启动至今
//   - 网络速率只统计「默认路由所在接口」，绝不聚合所有接口
//     （聚合会把 docker0 / br-* / ovs 桥与物理口重复累加，速率虚高）
//   - 局域网地址取默认路由接口的 IPv4，不取「第一个非回环地址」
//     （那样可能取到 docker0 的 172.17.0.1）
//   - 内存已用 = 总容量 - 可用，保证 已用 + 可用 == 总容量（三项自洽）
type SystemMonitor struct {
	CPUPercent float64
	CPUCores   []float64
	CPUCoreNum int
	CPUTemp    float64
	CPUHistory []float64

	MemPercent  float64
	MemUsedGB   float64
	MemTotalGB  float64
	MemAvailGB  float64
	MemHistory  []float64
	MemCacheGB  float64 // buff/cache（Buffers + Cached）
	SwapPercent float64
	SwapUsedGB  float64
	SwapTotalGB float64

	// 网络（仅主网口）
	NetIface    string
	NetSendMBps float64
	NetRecvMBps float64
	NetRxTotal  uint64 // 该网口自开机以来的累计接收字节
	NetTxTotal  uint64 // 该网口自开机以来的累计发送字节
	NetRxToday  uint64 // 今日累计接收字节（跨天自动归零）
	NetTxToday  uint64 // 今日累计发送字节
	NetHistory  [][2]float64

	LocalIP string

	// 系统
	UptimeSec float64   // /proc/uptime，系统开机至今秒数
	LoadAvg   [3]float64
	ProcCount int // 系统当前进程总数

	Disk *DiskMonitor

	lastRx, lastTx uint64
	lastNetTime    time.Time

	day *netDayState // 当日流量的起算基线
}

func NewSystemMonitor() *SystemMonitor {
	return &SystemMonitor{
		CPUHistory: make([]float64, 0, 30),
		MemHistory: make([]float64, 0, 30),
		NetHistory: make([][2]float64, 0, 30),
		Disk:       NewDiskMonitor(),
	}
}

func (s *SystemMonitor) UpdateCPU() error {
	percent, _ := cpu.Percent(0, false)
	if len(percent) > 0 {
		s.CPUPercent = percent[0]
		s.CPUHistory = append(s.CPUHistory, percent[0])
		if len(s.CPUHistory) > 30 {
			s.CPUHistory = s.CPUHistory[1:]
		}
	}

	// 首次采样可能返回空（没有上一帧基线），不要用空结果覆盖已有的核心数
	if cores, _ := cpu.Percent(0, true); len(cores) > 0 {
		s.CPUCores = cores
	}
	if n, err := cpu.Counts(true); err == nil && n > 0 {
		s.CPUCoreNum = n
	}

	// Go 1.19 的 gopsutil 没有 Temperature()，使用文件读取
	s.CPUTemp = s.getCPUTempFromFile()

	s.UptimeSec = readUptime()
	s.LoadAvg, _ = readLoadAvg()
	// 注意：/proc/loadavg 第四列的总数是「内核任务数」（含线程），
	// 明显大于真实进程数（实测 1240 vs 376）。要显示「进程」就必须数 /proc 里的 PID 目录。
	s.ProcCount = countProcesses()
	return nil
}

// countProcesses 数 /proc 下的纯数字目录，即真实进程数（与 `ps -e` 一致）
func countProcesses() int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		name := e.Name()
		if len(name) == 0 || name[0] < '0' || name[0] > '9' {
			continue
		}
		n++
	}
	return n
}

// readUptime 读 /proc/uptime 第一列（系统开机至今秒数）
func readUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// readLoadAvg 读 /proc/loadavg：前三个是 1/5/15 分钟负载，第四列 "运行数/总进程数"
func readLoadAvg() ([3]float64, int) {
	var la [3]float64
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return la, 0
	}
	f := strings.Fields(string(data))
	if len(f) < 4 {
		return la, 0
	}
	for i := 0; i < 3; i++ {
		la[i], _ = strconv.ParseFloat(f[i], 64)
	}
	if i := strings.Index(f[3], "/"); i >= 0 {
		if n, err := strconv.Atoi(f[3][i+1:]); err == nil {
			return la, n
		}
	}
	return la, 0
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
	total := float64(v.Total)
	avail := float64(v.Available)
	// 用「总 - 可用」作为已用，保证 已用 + 可用 == 总容量
	used := total - avail
	if used < 0 {
		used = 0
	}

	s.MemTotalGB = total / (1024 * 1024 * 1024)
	s.MemAvailGB = avail / (1024 * 1024 * 1024)
	s.MemUsedGB = used / (1024 * 1024 * 1024)
	if total > 0 {
		s.MemPercent = used / total * 100
	}

	s.MemHistory = append(s.MemHistory, s.MemPercent)
	if len(s.MemHistory) > 30 {
		s.MemHistory = s.MemHistory[1:]
	}

	// buff/cache 是内核页缓存，来自 /proc/meminfo 的真实读数
	s.MemCacheGB = float64(v.Buffers+v.Cached) / (1024 * 1024 * 1024)

	if swap, err := mem.SwapMemory(); err == nil {
		s.SwapPercent = swap.UsedPercent
		s.SwapUsedGB = float64(swap.Used) / (1024 * 1024 * 1024)
		s.SwapTotalGB = float64(swap.Total) / (1024 * 1024 * 1024)
	}
	return nil
}

// UpdateNetwork 只统计主网口（默认路由所在接口）的收发速率。
//
// 为什么不聚合所有接口：R1 上 enp2s0 被桥接进 ovs 桥（enp2s0-ovs），
// 同一份流量会同时记在物理口和桥上；再加上 docker0 / br-* 的容器流量，
// 聚合后速率会明显高于真实外网速率。
func (s *SystemMonitor) UpdateNetwork() error {
	if s.NetIface == "" {
		s.NetIface = defaultRouteIface()
		if s.NetIface == "" {
			return nil // 拿不到主网口就不编造数据
		}
		s.LocalIP = primaryIPv4(s.NetIface)
	}

	rx, tx, err := readIfaceCounters(s.NetIface)
	if err != nil {
		return err
	}

	// 当日累计（跨天自动归零，服务重启沿用当日基线）
	s.trackDay(rx, tx)

	now := time.Now()
	if s.lastNetTime.IsZero() {
		s.lastNetTime, s.lastRx, s.lastTx = now, rx, tx
		return nil
	}

	if elapsed := now.Sub(s.lastNetTime).Seconds(); elapsed > 0 {
		if rx >= s.lastRx {
			s.NetRecvMBps = float64(rx-s.lastRx) / elapsed / (1024 * 1024)
		} else {
			s.NetRecvMBps = 0 // 计数器回绕/接口重置，不显示负数
		}
		if tx >= s.lastTx {
			s.NetSendMBps = float64(tx-s.lastTx) / elapsed / (1024 * 1024)
		} else {
			s.NetSendMBps = 0
		}
		s.NetHistory = append(s.NetHistory, [2]float64{s.NetSendMBps, s.NetRecvMBps})
		if len(s.NetHistory) > 30 {
			s.NetHistory = s.NetHistory[1:]
		}
	}

	s.lastNetTime, s.lastRx, s.lastTx = now, rx, tx
	s.NetRxTotal, s.NetTxTotal = rx, tx
	return nil
}

// ------------------------------------------------------------
//  当日流量统计
// ------------------------------------------------------------
//
//  sysfs 的 rx_bytes / tx_bytes 是「接口自开机以来」的累计值，要得到「今日」
//  必须自己维护一条起算基线。规则：
//   - 跨天（日期变了）→ 基线重置为当前计数，并落盘
//   - 服务在本日内重启 → 读回落盘的基线继续累加（不会把当天已产生的流量丢掉）
//   - 计数器回绕 / 接口 reset（当前值 < 基线）→ 基线重置
//  基线落盘失败只影响重启后的精度，不影响当前显示。

type netDayState struct {
	Date string `json:"date"`
	Rx   uint64 `json:"rx"`
	Tx   uint64 `json:"tx"`
}

const netDayFile = "/var/lib/r1-toolbox/net-day.json"

func (s *SystemMonitor) trackDay(rx, tx uint64) {
	today := time.Now().Format("2006-01-02")

	switch {
	case s.day == nil:
		// 首次采样：优先沿用当天已落盘的基线（服务重启场景）
		if st, ok := loadNetDay(); ok && st.Date == today && rx >= st.Rx && tx >= st.Tx {
			s.day = &st
		} else {
			s.day = &netDayState{Date: today, Rx: rx, Tx: tx}
			saveNetDay(*s.day)
		}
	case s.day.Date != today || rx < s.day.Rx || tx < s.day.Tx:
		// 跨天 或 计数器重置
		s.day = &netDayState{Date: today, Rx: rx, Tx: tx}
		saveNetDay(*s.day)
	}

	s.NetRxToday = rx - s.day.Rx
	s.NetTxToday = tx - s.day.Tx
	s.NetRxTotal = rx
	s.NetTxTotal = tx
}

func loadNetDay() (netDayState, bool) {
	var st netDayState
	b, err := os.ReadFile(netDayFile)
	if err != nil {
		return st, false
	}
	if json.Unmarshal(b, &st) != nil || st.Date == "" {
		return netDayState{}, false
	}
	return st, true
}

func saveNetDay(st netDayState) {
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(netDayFile), 0755) != nil {
		return
	}
	os.WriteFile(netDayFile, b, 0644)
}

// defaultRouteIface 解析 /proc/net/route 取默认路由接口（Destination == 00000000）
func defaultRouteIface() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[1] == "00000000" {
			return f[0]
		}
	}
	return ""
}

// primaryIPv4 取指定网口的第一个 IPv4 地址
func primaryIPv4(iface string) string {
	ifi, err := netLib.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*netLib.IPNet); ok {
			if ip4 := ipn.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

// readIfaceCounters 直接读 sysfs 的接口字节计数（比 gopsutil 聚合更可控）
func readIfaceCounters(iface string) (rx, tx uint64, err error) {
	read := func(name string) uint64 {
		b, e := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics", name))
		if e != nil {
			return 0
		}
		v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		return v
	}
	rx = read("rx_bytes")
	tx = read("tx_bytes")
	if rx == 0 && tx == 0 {
		return 0, 0, fmt.Errorf("读取 %s 收发计数失败", iface)
	}
	return rx, tx, nil
}

func (s *SystemMonitor) UpdateDisk() error {
	return s.Disk.Update()
}
