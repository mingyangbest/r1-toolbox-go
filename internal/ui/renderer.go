package ui

import (
	"fmt"
	"log"
	"os"

	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"

	"r1-toolbox/internal/config"
	"r1-toolbox/internal/monitor"
)

type Renderer struct {
	window     *sdl.Window
	renderer   *sdl.Renderer
	font       *ttf.Font
	monitor    *monitor.SystemMonitor
	background *sdl.Texture
	textColor  sdl.Color
}

func NewRenderer(mon *monitor.SystemMonitor) (*Renderer, error) {
	os.Setenv("SDL_FBDEV", "/dev/fb0")
	
	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		return nil, err
	}
	if err := ttf.Init(); err != nil {
		return nil, err
	}
	
	log.Println("尝试创建 SDL 窗口...")
	window, err := sdl.CreateWindow("R1 Toolbox", 
		0, 0,
		config.ScreenWidth, config.ScreenHeight, 
		sdl.WINDOW_SHOWN)
	if err != nil {
		return nil, fmt.Errorf("创建窗口失败: %v", err)
	}
	
	log.Println("创建渲染器...")
	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_SOFTWARE)
	if err != nil {
		return nil, fmt.Errorf("创建渲染器失败: %v", err)
	}
	
	log.Println("加载字体...")
	// 尝试多个中文字体路径，字体大小改为24
	fontPaths := []string{
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		"/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
		"/usr/share/fonts/wqy-microhei/wqy-microhei.ttc",
		"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	}
	
	var font *ttf.Font
	for _, path := range fontPaths {
		font, err = ttf.OpenFont(path, 24)
		if err == nil {
			log.Printf("成功加载字体: %s", path)
			break
		}
	}
	if font == nil {
		log.Printf("警告: 所有字体加载失败，中文可能无法显示")
	}
	
	log.Println("SDL2 渲染器初始化成功")
	
	return &Renderer{
		window:     window,
		renderer:   renderer,
		font:       font,
		monitor:    mon,
		background: nil,
		textColor:  sdl.Color{R: 255, G: 255, B: 255, A: 255},
	}, nil
}

func (r *Renderer) DrawFrame() {
	if r.background != nil {
		r.renderer.Copy(r.background, nil, nil)
	} else {
		r.renderer.SetDrawColor(20, 22, 28, 255)
		r.renderer.Clear()
	}
	
	const gap = 12
	const cardW = 336
	const cardH = 180
	
	y := int32(gap)
	r.drawCPUCard(20, y, cardW, cardH)
	y += cardH + gap
	
	r.drawMemoryCard(20, y, cardW, cardH)
	y += cardH + gap
	
	r.drawDiskCard(20, y, cardW, cardH)
	y += cardH + gap
	
	r.drawNetworkCard(20, y, cardW, cardH)
	y += cardH + gap
	
	r.drawProcessCard(20, y, cardW, cardH)
	
	r.renderer.Present()
}

func (r *Renderer) drawCPUCard(x, y, w, h int32) {
	r.drawCard(x, y, w, h)
	r.drawText(fmt.Sprintf("CPU: %.1f%%", r.monitor.CPUPercent), x+15, y+15)
	r.drawText(fmt.Sprintf("温度: %.1f°C", r.monitor.CPUTemp), x+15, y+50)
	
	coreY := y + 85
	for i, core := range r.monitor.CPUCores {
		if i >= 4 {
			break
		}
		text := fmt.Sprintf("C%d: %.0f%%", i+1, core)
		r.drawText(text, x+15+int32(i%2)*170, coreY+int32(i/2)*30)
	}
}

func (r *Renderer) drawMemoryCard(x, y, w, h int32) {
	r.drawCard(x, y, w, h)
	r.drawText(fmt.Sprintf("内存: %.1f%%", r.monitor.MemPercent), x+15, y+15)
	r.drawText(fmt.Sprintf("%.1fG/%.1fG", r.monitor.MemUsedGB, r.monitor.MemTotalGB), x+15, y+50)
	r.drawText(fmt.Sprintf("可用: %.1fG", r.monitor.MemAvailGB), x+15, y+85)
	r.drawText(fmt.Sprintf("SWAP: %.1f%%", r.monitor.SwapPercent), x+15, y+120)
}

func (r *Renderer) drawNetworkCard(x, y, w, h int32) {
	r.drawCard(x, y, w, h)
	r.drawText(fmt.Sprintf("上传: %.2f MB/s", r.monitor.NetSendMBps), x+15, y+15)
	r.drawText(fmt.Sprintf("下载: %.2f MB/s", r.monitor.NetRecvMBps), x+15, y+50)
	r.drawText(fmt.Sprintf("IP: %s", r.monitor.LocalIP), x+15, y+85)
}


func (r *Renderer) drawDiskCard(x, y, w, h int32) {
	r.drawCard(x, y, w, h)
	
	disks := r.monitor.Disk.GetVolumes()
	if len(disks) == 0 {
		r.drawText("无磁盘", x+15, y+15)
		return
	}
	
	dy := y + 15
	for i, disk := range disks {
		if i >= 3 {
			break
		}
		text := fmt.Sprintf("%s: %.0f%% %.0fG/%s", 
			disk.VolName, disk.UsedPercent, disk.UsedGB, disk.DeviceType)
		r.drawText(text, x+15, dy)
		
		if disk.Temp > 0 {
			tempText := fmt.Sprintf("%.0f°C", disk.Temp)
			r.drawText(tempText, x+w-60, dy)
		}
		dy += 50
	}
}

func (r *Renderer) drawProcessCard(x, y, w, h int32) {
	r.drawCard(x, y, w, h)
	r.drawText("进程", x+15, y+15)
	
	dy := y + 50
	for i, proc := range r.monitor.Processes {
		if i >= 3 {
			break
		}
		name := proc.Name
		if len(name) > 12 {
			name = name[:12]
		}
		text := fmt.Sprintf("%s %.1f%%", name, proc.CPUPercent)
		r.drawText(text, x+15, dy)
		dy += 40
	}
}

func (r *Renderer) drawCard(x, y, w, h int32) {
	rect := sdl.Rect{X: x, Y: y, W: w, H: h}
	r.renderer.SetDrawColor(30, 30, 35, 120)
	r.renderer.FillRect(&rect)
}

func (r *Renderer) drawText(text string, x, y int32) {
	if r.font == nil {
		return
	}
	surface, _ := r.font.RenderUTF8Blended(text, r.textColor)
	if surface != nil {
		defer surface.Free()
		texture, _ := r.renderer.CreateTextureFromSurface(surface)
		if texture != nil {
			defer texture.Destroy()
			r.renderer.Copy(texture, nil, &sdl.Rect{X: x, Y: y, W: surface.W, H: surface.H})
		}
	}
}

func (r *Renderer) Cleanup() {
	if r.font != nil {
		r.font.Close()
	}
	if r.renderer != nil {
		r.renderer.Destroy()
	}
	if r.window != nil {
		r.window.Destroy()
	}
	ttf.Quit()
	sdl.Quit()
}


func (r *Renderer) LoadBackground(path string) error {
	if r.background != nil {
		r.background.Destroy()
		r.background = nil
	}
	
	surface, err := LoadBackground(path, config.ScreenWidth, config.ScreenHeight)
	if err != nil {
		return err
	}
	defer surface.Free()
	
	texture, err := r.renderer.CreateTextureFromSurface(surface)
	if err != nil {
		return err
	}
	
	r.background = texture
	return nil
}

func (r *Renderer) SetTextColor(color string) {
	r.textColor = parseColor(color)
}

func parseColor(hex string) sdl.Color {
	if len(hex) != 7 || hex[0] != '#' {
		return sdl.Color{R: 255, G: 255, B: 255, A: 255}
	}
	
	var r, g, b uint8
	fmt.Sscanf(hex[1:3], "%02x", &r)
	fmt.Sscanf(hex[3:5], "%02x", &g)
	fmt.Sscanf(hex[5:7], "%02x", &b)
	
	return sdl.Color{R: r, G: g, B: b, A: 255}
}


func (r *Renderer) drawTextLarge(text string, x, y int32) {
	if r.font == nil {
		return
	}
	
	largeFontPaths := []string{
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		"/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
	}
	
	var largeFont *ttf.Font
	for _, path := range largeFontPaths {
		largeFont, _ = ttf.OpenFont(path, 48)
		if largeFont != nil {
			break
		}
	}
	if largeFont == nil {
		r.drawText(text, x, y)
		return
	}
	defer largeFont.Close()
	
	surface, _ := largeFont.RenderUTF8Blended(text, r.textColor)
	if surface != nil {
		defer surface.Free()
		texture, _ := r.renderer.CreateTextureFromSurface(surface)
		if texture != nil {
			defer texture.Destroy()
			r.renderer.Copy(texture, nil, &sdl.Rect{X: x, Y: y, W: surface.W, H: surface.H})
		}
	}
}


func (r *Renderer) DrawPage(page Page, screenOff bool) {
	if screenOff {
		// 屏幕关闭时绘制全黑画面
		r.renderer.SetDrawColor(0, 0, 0, 255)
		r.renderer.Clear()
	} else {
		if r.background != nil {
			r.renderer.Copy(r.background, nil, nil)
		} else {
			r.renderer.SetDrawColor(20, 22, 28, 255)
			r.renderer.Clear()
		}
		page.Draw(r)
	}
	r.renderer.Present()
}
