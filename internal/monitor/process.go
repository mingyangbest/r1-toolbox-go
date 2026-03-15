package monitor

import (
	"fmt"
	"sort"

	"github.com/shirou/gopsutil/v3/process"
)

type ProcessInfo struct {
	PID         int32
	Name        string
	CPUPercent  float64
	MemoryMB    float64
	MemoryStr   string
}

func GetTopProcesses(limit int) []ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return []ProcessInfo{}
	}
	
	var result []ProcessInfo
	for _, p := range procs {
		cpu, _ := p.CPUPercent()
		mem, _ := p.MemoryInfo()
		name, _ := p.Name()
		
		if cpu < 0.1 {
			continue
		}
		
		memMB := float64(mem.RSS) / (1024 * 1024)
		memStr := formatMemory(mem.RSS)
		
		result = append(result, ProcessInfo{
			PID:        p.Pid,
			Name:       name,
			CPUPercent: cpu,
			MemoryMB:   memMB,
			MemoryStr:  memStr,
		})
	}
	
	sort.Slice(result, func(i, j int) bool {
		return result[i].CPUPercent > result[j].CPUPercent
	})
	
	if len(result) > limit {
		result = result[:limit]
	}
	
	return result
}

func formatMemory(bytes uint64) string {
	mb := float64(bytes) / (1024 * 1024)
	if mb < 1024 {
		return fmt.Sprintf("%.1f MB", mb)
	}
	return fmt.Sprintf("%.1f GB", mb/1024)
}
