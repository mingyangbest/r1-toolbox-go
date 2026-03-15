package monitor

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
)

type DiskMonitor struct {
	volumes []VolumeInfo
}

type VolumeInfo struct {
	Mount       string
	Device      string
	UsedPercent float64
	UsedGB      float64
	TotalGB     float64
	Temp        float64
	DeviceType  string
	VolName     string
}

func NewDiskMonitor() *DiskMonitor {
	return &DiskMonitor{}
}

func (d *DiskMonitor) Update() error {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return err
	}
	
	d.volumes = []VolumeInfo{}
	for _, p := range partitions {
		if !isAllowedMount(p.Mountpoint) {
			continue
		}
		
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		
		temp := getDiskTemp(p.Device)
		devType := getDiskType(p.Device)
		
		d.volumes = append(d.volumes, VolumeInfo{
			Mount:       p.Mountpoint,
			Device:      p.Device,
			UsedPercent: usage.UsedPercent,
			UsedGB:      float64(usage.Used) / (1024 * 1024 * 1024),
			TotalGB:     float64(usage.Total) / (1024 * 1024 * 1024),
			Temp:        temp,
			DeviceType:  devType,
			VolName:     getVolName(p.Mountpoint),
		})
	}
	return nil
}

func (d *DiskMonitor) GetVolumes() []VolumeInfo {
	return d.volumes
}

func isAllowedMount(mount string) bool {
	allowed := []string{"/", "/vol1", "/vol2", "/vol3", "/vol4", "/media", "/mnt"}
	for _, prefix := range allowed {
		if strings.HasPrefix(mount, prefix) {
			return true
		}
	}
	return false
}

func getVolName(mount string) string {
	if mount == "/" {
		return "FnOS"
	}
	parts := strings.Split(mount, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return mount
}

func getDiskTemp(device string) float64 {
	re := regexp.MustCompile(`/dev/(\w+)`)
	match := re.FindStringSubmatch(device)
	if len(match) < 2 {
		return 0
	}
	
	devName := match[1]
	devName = regexp.MustCompile(`\d+$`).ReplaceAllString(devName, "")
	
	cmd := exec.Command("smartctl", "-A", "/dev/"+devName)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	
	tempRe := regexp.MustCompile(`194 Temperature_Celsius\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`)
	match = tempRe.FindStringSubmatch(string(out))
	if len(match) > 1 {
		temp, _ := strconv.ParseFloat(match[1], 64)
		return temp
	}
	return 0
}

func getDiskType(device string) string {
	if strings.Contains(device, "nvme") {
		return "SSD"
	}
	
	re := regexp.MustCompile(`/dev/(\w+)`)
	match := re.FindStringSubmatch(device)
	if len(match) < 2 {
		return "HDD"
	}
	
	devName := match[1]
	devName = regexp.MustCompile(`\d+$`).ReplaceAllString(devName, "")
	
	cmd := exec.Command("lsblk", "-d", "-o", "ROTA", "/dev/"+devName)
	out, err := cmd.Output()
	if err != nil {
		return "HDD"
	}
	
	if strings.Contains(string(out), "0") {
		return "SSD"
	}
	return "HDD"
}
