package monitor

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
)

// DiskMonitor 采集存储卷信息。
//
// 数据真实性约定：
//   - 卷名优先用磁盘真实卷标（blkid/lsblk 的 LABEL），取不到才退回挂载点的语义名
//     （旧版本对 "/" 硬编码返回 "FnOS"，那是编造的名字，已移除）
//   - 介质类型沿设备树追溯到最底层物理盘，读 sysfs 的 queue/rotational
//     旧版本对 LVM mapper 设备直接返回 HDD，导致 NVMe 上的 /vol1 被误标成 HDD
//   - 温度优先读 sysfs hwmon（NVMe 走这里），SATA 兜底用 smartctl 的 194 项
type DiskMonitor struct {
	// mu 保护 volumes。Update 在采样协程每 2s 重建切片，而
	// MaxDiskTemp/GetVolumes 分别被风扇控制协程与渲染协程并发读取
	// （-race 曾在此实锤 DATA RACE，2026-09-27 修复）。
	mu      sync.RWMutex
	volumes []VolumeInfo
}

type VolumeInfo struct {
	Mount       string
	Device      string // 分区设备，如 /dev/nvme0n1p2
	BaseDevice  string // 底层物理盘，如 nvme0n1 / sda
	UsedPercent float64
	UsedGB      float64
	TotalGB     float64
	UsedBytes   uint64 // 原始字节（始终保留，便于与 df 对账）
	TotalBytes  uint64
	Temp        float64
	DeviceType  string // NVMe SSD / SSD / HDD / eMMC
	VolName     string // 展示名：真实卷标 → 语义名 → 挂载点末段
	Label       string // 真实 LABEL，可能为空
}

func NewDiskMonitor() *DiskMonitor {
	return &DiskMonitor{}
}

func (d *DiskMonitor) Update() error {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return err
	}

	// 在局部切片上构建，最后一把写锁换入 —— 让写临界区不覆盖
	// smartctl 等慢调用，风扇/渲染协程的读锁最长只等一次指针赋值。
	vols := []VolumeInfo{}
	seen := map[string]bool{}

	for _, p := range partitions {
		if !isRealVolume(p.Mountpoint) || seen[p.Mountpoint] {
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}

		totalGB := float64(usage.Total) / (1024 * 1024 * 1024)
		if totalGB < 1 {
			continue // 引导分区（/boot/efi 190M 之类）不是存储卷
		}

		base := baseDevice(p.Device)
		if strings.HasPrefix(base, "mmcblk") {
			continue // 读卡器里的可移除介质不占用「存储卷」名额
		}

		seen[p.Mountpoint] = true
		label := readLabel(p.Device)
		// 百分比自己算，不用 gopsutil 的 usage.UsedPercent。
		//
		// gopsutil(v3.24.1) 的 UsedPercent 用的是 df 的 Use% 口径：
		//     UsedPercent = Used / (Used + Bavail)
		// 分母把 ext4 给 root 预留的块（默认 5%）排除在外；而卡片上写的是
		// 「已用 / 总容量」，隐含的分母是 Total。两个基准不同，实测在 / 上
		// 会差 1.24 个点（24.21% vs 22.97%），于是环形弧和旁边的大字对不上。
		// 统一成 Used/Total，保证环上的数字与旁边两个容量数永远自洽，
		// 并且可以直接和 `df -B1` 的字节数逐条对账。
		pct := 0.0
		if usage.Total > 0 {
			pct = float64(usage.Used) / float64(usage.Total) * 100
		}
		vols = append(vols, VolumeInfo{
			Mount:       p.Mountpoint,
			Device:      p.Device,
			BaseDevice:  base,
			UsedPercent: pct,
			UsedGB:      float64(usage.Used) / (1024 * 1024 * 1024),
			TotalGB:     totalGB,
			UsedBytes:   usage.Used,
			TotalBytes:  usage.Total,
			Temp:        diskTemp(base),
			DeviceType:  diskType(base),
			VolName:     volumeName(p.Mountpoint, label),
			Label:       label,
		})
	}

	// 顺序稳定：系统盘（/）永远第一，其余按容量从大到小
	sort.Slice(vols, func(i, j int) bool {
		a, b := vols[i], vols[j]
		if (a.Mount == "/") != (b.Mount == "/") {
			return a.Mount == "/"
		}
		return a.TotalGB > b.TotalGB
	})

	// 审计日志：每 60 秒一条，把原始字节数落盘，
	// 屏幕上的百分比随时可以在 journalctl 里与 `df -B1` 逐条对账。
	for _, v := range vols {
		log.Printf("[disk] %s (%s @ %s) used=%d total=%d = %.2f/%.2f GB  %.2f%%  %s",
			v.VolName, v.BaseDevice, v.Mount, v.UsedBytes, v.TotalBytes,
			v.UsedGB, v.TotalGB, v.UsedPercent, v.DeviceType)
	}

	d.mu.Lock()
	d.volumes = vols
	d.mu.Unlock()
	return nil
}

// GetVolumes 返回卷列表的副本 —— 调用方（渲染协程、Web 面板）拿到的是
// 快照，后续 Update 重建不会影响它，调用方也不该有能力改内部状态。
func (d *DiskMonitor) GetVolumes() []VolumeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]VolumeInfo, len(d.volumes))
	copy(out, d.volumes)
	return out
}

// MaxDiskTemp 返回所有存储卷盘体温度里的**最高值**（0 表示一个都读不到）。
//
// 用途：机箱风扇的自动调速被控量。取最高而不是平均值 ——
// 调速只需要照顾最热的那块盘，只要它没超，其他盘必然也没超。
// 注意这是**盘体自身**的温度（NVMe 走 sysfs hwmon、SATA 走 smartctl 194 项），
// 不是 IT8628 那颗位于主板上的传感器：后者常年比硬盘高 10°C 左右，
// 拿它当被控量会让风扇永远顶在满速（旧版就是这样）。
func (d *DiskMonitor) MaxDiskTemp() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	mx := 0.0
	for _, v := range d.volumes {
		if v.Temp > mx {
			mx = v.Temp
		}
	}
	return mx
}

// isRealVolume 排除系统挂载点，只保留真正的存储卷
func isRealVolume(mount string) bool {
	if mount == "/" {
		return true
	}
	for _, p := range []string{"/boot", "/dev", "/proc", "/run", "/sys", "/snap"} {
		if mount == p || strings.HasPrefix(mount, p+"/") {
			return false
		}
	}
	return strings.HasPrefix(mount, "/")
}

// baseDevice 沿设备树回溯到最底层物理盘名（nvme0n1 / sda）
func baseDevice(device string) string {
	name := filepath.Base(device)

	// /dev/mapper/xxx 在 /sys/class/block 里只以 dm-N 出现，先换名
	if strings.HasPrefix(device, "/dev/mapper/") {
		if n := dmNodeByAlias(name); n != "" {
			name = n
		}
	}

	// 沿 slaves 一路向下：dm-N -> md0 -> nvme0n1p3
	for i := 0; i < 8; i++ {
		entries, err := os.ReadDir(filepath.Join("/sys/class/block", name, "slaves"))
		if err != nil || len(entries) == 0 {
			break
		}
		name = entries[0].Name()
	}

	// 若停在分区上，按 sysfs 真实路径回溯到整盘。
	//
	// 这里不能用 lsblk -no PKNAME：RAID 成员分区（例如被 md0 占用的 nvme0n1p3）
	// 会被 lsblk 报成它自己，于是整盘名一路取错（曾导致 vol1 显示成 nvme0n1p3）。
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(filepath.Join("/sys/class/block", name, "partition")); err != nil {
			break
		}
		parent := parentBlockOf(name)
		if parent == "" || parent == name {
			break
		}
		name = parent
	}
	return name
}

// parentBlockOf 由 sysfs 真实路径取分区所属的整盘名
func parentBlockOf(part string) string {
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", part))
	if err != nil {
		return ""
	}
	// /sys/devices/.../block/nvme0n1/nvme0n1p3 → 父目录名即整盘
	return filepath.Base(filepath.Dir(p))
}

// dmNodeByAlias 把 device-mapper 别名（trim_xxx-0）换成内核节点名（dm-1）
func dmNodeByAlias(alias string) string {
	entries, err := filepath.Glob("/sys/class/block/dm-*")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(e, "dm", "name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == alias {
			return filepath.Base(e)
		}
	}
	return ""
}

// diskType 读 sysfs 的 rotational 判断机械/固态（最可靠，不猜设备名）
func diskType(base string) string {
	switch {
	case strings.HasPrefix(base, "nvme"):
		return "NVMe SSD"
	case strings.HasPrefix(base, "mmcblk"):
		return "eMMC"
	}
	b, err := os.ReadFile(filepath.Join("/sys/block", base, "queue/rotational"))
	if err != nil {
		return "磁盘"
	}
	if strings.TrimSpace(string(b)) == "0" {
		return "SSD"
	}
	return "HDD"
}

// smartctl 是外部进程，单次 10~40ms。2 秒采样周期里每块 SATA 盘都跑一次
// 是本文件最贵的一笔账（J2 优化点，2026-09-27）。盘温物理变化速度是分钟级，
// 30 秒 TTL 对显示与风扇控制都毫无感知差异；NVMe 走 sysfs 读文件，
// 微秒级，不值得缓存，保持实时。
const smartTempTTL = 30 * time.Second

type tempEntry struct {
	temp float64
	at   time.Time
}

var (
	smartTempMu sync.Mutex
	smartTempC  = map[string]tempEntry{}
)

// diskTemp 优先 sysfs（NVMe），SATA 盘兜底用 smartctl（带 30s TTL 缓存）
func diskTemp(base string) float64 {
	paths, _ := filepath.Glob(filepath.Join("/sys/block", base, "device/hwmon*/temp1_input"))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && v > 0 {
			return float64(v) / 1000.0
		}
	}

	// sysfs 拿不到 → SATA 走 smartctl。命中未过期缓存直接返回，
	// 过期才真正起子进程。
	smartTempMu.Lock()
	if e, ok := smartTempC[base]; ok && time.Since(e.at) < smartTempTTL {
		t := e.temp
		smartTempMu.Unlock()
		return t
	}
	smartTempMu.Unlock()

	out, err := exec.Command("smartctl", "-A", "/dev/"+base).Output()
	if err != nil {
		return 0
	}
	t := parseSmartTemp(string(out))
	if t > 0 {
		smartTempMu.Lock()
		smartTempC[base] = tempEntry{temp: t, at: time.Now()}
		smartTempMu.Unlock()
	}
	return t
}

var (
	// SATA：194 Temperature_Celsius ... 37 (Min/Max 2/65)
	reTempAttr = regexp.MustCompile(`Temperature_Celsius.*?(\d+)\s*\(Min`)
	// NVMe：Temperature: 48 Celsius
	reTempNVMe = regexp.MustCompile(`(?m)^Temperature:\s+(\d+)\s+Celsius`)
)

func parseSmartTemp(out string) float64 {
	for _, re := range []*regexp.Regexp{reTempAttr, reTempNVMe} {
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
				return float64(v)
			}
		}
	}
	return 0
}

// readLabel 读分区真实卷标（无卷标返回空串）
func readLabel(device string) string {
	out, err := exec.Command("lsblk", "-no", "LABEL", device).Output()
	if err != nil {
		return ""
	}
	return firstLine(out)
}

// volumeName 展示名：真实卷标 > 系统盘语义名 > 挂载点末段
func volumeName(mount, label string) string {
	if label != "" {
		return label
	}
	if mount == "/" {
		return "系统盘"
	}
	return filepath.Base(mount)
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
