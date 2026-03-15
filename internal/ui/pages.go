package ui

import (
	"fmt"
	"strings"

	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
)

type Page interface {
	Draw(r *Renderer)
}

type CPUPage struct {
	mon *monitor.SystemMonitor
}

func NewCPUPage(mon *monitor.SystemMonitor) *CPUPage {
	return &CPUPage{mon: mon}
}

func (p *CPUPage) Draw(r *Renderer) {
	r.drawCard(20, 100, 336, 760)
	r.drawTextLarge(fmt.Sprintf("CPU: %.1f%%", p.mon.CPUPercent), 40, 150)
	r.drawTextLarge(fmt.Sprintf("温度: %.1f°C", p.mon.CPUTemp), 40, 250)
	
	y := int32(350)
	for i, core := range p.mon.CPUCores {
		if i >= 4 {
			break
		}
		text := fmt.Sprintf("核心%d: %.0f%%", i+1, core)
		r.drawTextLarge(text, 40, y)
		y += 80
	}
}

type MemoryPage struct {
	mon *monitor.SystemMonitor
}

func NewMemoryPage(mon *monitor.SystemMonitor) *MemoryPage {
	return &MemoryPage{mon: mon}
}

func (p *MemoryPage) Draw(r *Renderer) {
	r.drawCard(20, 100, 336, 760)
	r.drawTextLarge(fmt.Sprintf("内存: %.1f%%", p.mon.MemPercent), 40, 150)
	r.drawTextLarge(fmt.Sprintf("%.1fG / %.1fG", p.mon.MemUsedGB, p.mon.MemTotalGB), 40, 250)
	r.drawTextLarge(fmt.Sprintf("可用: %.1fG", p.mon.MemAvailGB), 40, 350)
	r.drawTextLarge(fmt.Sprintf("SWAP: %.1f%%", p.mon.SwapPercent), 40, 450)
}

type NetworkPage struct {
	mon *monitor.SystemMonitor
}

func NewNetworkPage(mon *monitor.SystemMonitor) *NetworkPage {
	return &NetworkPage{mon: mon}
}

func (p *NetworkPage) Draw(r *Renderer) {
	r.drawCard(20, 100, 336, 760)
	// 分两行显示上传速度
	r.drawTextLarge(fmt.Sprintf("上传: %.2f", p.mon.NetSendMBps), 40, 150)
	r.drawText("MB/s", 40, 210)
	
	// 分两行显示下载速度
	r.drawTextLarge(fmt.Sprintf("下载: %.2f", p.mon.NetRecvMBps), 40, 280)
	r.drawText("MB/s", 40, 340)
	
	// IP地址每行一个
	r.drawText("IP地址:", 40, 420)
	ips := strings.Split(p.mon.LocalIP, ", ")
	y := int32(460)
	for _, ip := range ips {
		if y > 800 {
			break
		}
		r.drawText(ip, 40, y)
		y += 40
	}
}

type DiskPage struct {
	mon *monitor.SystemMonitor
}

func NewDiskPage(mon *monitor.SystemMonitor) *DiskPage {
	return &DiskPage{mon: mon}
}

func (p *DiskPage) Draw(r *Renderer) {
	r.drawCard(20, 100, 336, 760)
	
	disks := p.mon.Disk.GetVolumes()
	if len(disks) == 0 {
		r.drawTextLarge("无磁盘", 40, 150)
		return
	}
	
	y := int32(150)
	for i, disk := range disks {
		if i >= 5 {
			break
		}
		text := fmt.Sprintf("%s: %.0f%%", disk.VolName, disk.UsedPercent)
		r.drawTextLarge(text, 40, y)
		y += 70
		
		sizeText := fmt.Sprintf("%.0fG / %s", disk.UsedGB, disk.DeviceType)
		r.drawText(sizeText, 40, y)
		y += 50
		
		if disk.Temp > 0 {
			tempText := fmt.Sprintf("温度: %.0f°C", disk.Temp)
			r.drawText(tempText, 40, y)
			y += 50
		}
	}
}

type FanPage struct {
	mon     *monitor.SystemMonitor
	fanCtrl *hardware.FanController
}

func NewFanPage(mon *monitor.SystemMonitor, fanCtrl *hardware.FanController) *FanPage {
	return &FanPage{mon: mon, fanCtrl: fanCtrl}
}

func (p *FanPage) Draw(r *Renderer) {
	r.drawCard(20, 100, 336, 760)
	r.drawTextLarge("风扇控制", 40, 150)
	
	// CPU风扇
	cpuMode := "自动"
	if p.fanCtrl != nil {
		switch p.fanCtrl.GetCPUMode() {
		case "low":
			cpuMode = "低速"
		case "medium":
			cpuMode = "中速"
		case "high":
			cpuMode = "高速"
		}
	}
	r.drawText("CPU风扇:", 40, 250)
	r.drawTextLarge(cpuMode, 40, 290)
	
	// HDD风扇
	hddMode := "自动"
	if p.fanCtrl != nil {
		switch p.fanCtrl.GetHDDMode() {
		case "low":
			hddMode = "低速"
		case "medium":
			hddMode = "中速"
		case "high":
			hddMode = "高速"
		}
	}
	r.drawText("机箱风扇:", 40, 450)
	r.drawTextLarge(hddMode, 40, 490)
	
	r.drawText("点击切换模式", 40, 700)
}

func (p *FanPage) HandleTap(x, y int32, fanCtrl *hardware.FanController) {
	// CPU风扇区域 (y: 250-400)
	if y >= 250 && y <= 400 {
		p.cycleCPUMode(fanCtrl)
	}
	// HDD风扇区域 (y: 450-600)
	if y >= 450 && y <= 600 {
		p.cycleHDDMode(fanCtrl)
	}
}

func (p *FanPage) cycleCPUMode(fanCtrl *hardware.FanController) {
	if fanCtrl == nil {
		return
	}
	modes := []string{"auto", "low", "medium", "high"}
	current := fanCtrl.GetCPUMode()
	for i, m := range modes {
		if m == current {
			next := modes[(i+1)%len(modes)]
			fanCtrl.SetCPUMode(next)
			break
		}
	}
}

func (p *FanPage) cycleHDDMode(fanCtrl *hardware.FanController) {
	if fanCtrl == nil {
		return
	}
	modes := []string{"auto", "low", "medium", "high"}
	current := fanCtrl.GetHDDMode()
	for i, m := range modes {
		if m == current {
			next := modes[(i+1)%len(modes)]
			fanCtrl.SetHDDMode(next)
			break
		}
	}
}

type ScreenOffPage struct {
	brightness *hardware.BrightnessController
}

func NewScreenOffPage(brightness *hardware.BrightnessController) *ScreenOffPage {
	return &ScreenOffPage{brightness: brightness}
}

func (p *ScreenOffPage) Draw(r *Renderer) {
	r.drawCard(20, 300, 336, 360)
	r.drawTextLarge("关闭屏幕", 80, 420)
	r.drawText("点击此处关闭屏幕", 80, 540)
}

func (p *ScreenOffPage) HandleTap(x, y int32) {
	// 整个卡片区域都可以点击
	if x >= 20 && x <= 356 && y >= 300 && y <= 660 {
		if p.brightness != nil {
			p.brightness.Set(0)
		}
	}
}
