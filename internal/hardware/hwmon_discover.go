package hardware

import (
	"os"
	"path/filepath"
	"strings"
)

func DiscoverHwmonPath(chipName string) string {
	hwmonBase := "/sys/class/hwmon"
	entries, err := os.ReadDir(hwmonBase)
	if err != nil {
		return ""
	}
	
	for _, entry := range entries {
		namePath := filepath.Join(hwmonBase, entry.Name(), "name")
		data, err := os.ReadFile(namePath)
		if err != nil {
			continue
		}
		
		name := strings.TrimSpace(string(data))
		if strings.Contains(name, chipName) {
			return filepath.Join(hwmonBase, entry.Name())
		}
	}
	return ""
}

func DiscoverCoretempPath() string {
	return DiscoverHwmonPath("coretemp")
}

func DiscoverIT8628Path() string {
	path := DiscoverHwmonPath("it87")
	if path == "" {
		path = DiscoverHwmonPath("it8628")
	}
	return path
}

func DiscoverNVMePath() string {
	return DiscoverHwmonPath("nvme")
}
