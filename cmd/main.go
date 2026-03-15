package main

import (
	"fmt"
	"log"
	"time"

	"github.com/veandco/go-sdl2/sdl"

	"r1-toolbox/internal/config"
	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
	"r1-toolbox/internal/ui"
	"r1-toolbox/internal/web"
)

func main() {
	log.Println("R1 Toolbox 启动中...")
	
	cfg, err := config.Load()
	if err != nil {
		log.Printf("加载配置失败，使用默认配置: %v", err)
		cfg = config.DefaultConfig()
	}
	
	mon := monitor.NewSystemMonitor()
	
	fanCtrl := hardware.NewFanController()
	fanCtrl.CPUMinTemp = cfg.Fan.CPU.MinTemp
	fanCtrl.CPUMaxTemp = cfg.Fan.CPU.MaxTemp
	fanCtrl.HDDMinTemp = cfg.Fan.HDD.MinTemp
	fanCtrl.HDDMaxTemp = cfg.Fan.HDD.MaxTemp
	fanCtrl.SetCPUMode(cfg.Fan.CPU.Mode)
	fanCtrl.SetHDDMode(cfg.Fan.HDD.Mode)
	fanCtrl.Init()
	
	rgbCtrl := hardware.NewRGBController()
	rgbCtrl.SetMode(cfg.RGB.Mode, cfg.RGB.Color)
	
	brightness := hardware.NewBrightnessController(cfg.Hardware.BrightnessPath, 0, 96000)
	brightness.Set(cfg.Screen.Brightness)
	
	go fanCtrl.Run()
	
	renderer, err := ui.NewRenderer(mon)
	if err != nil {
		log.Fatal(err)
	}
	defer renderer.Cleanup()
	
	if cfg.Screen.Background != "" {
		if err := renderer.LoadBackground(cfg.Screen.Background); err != nil {
			log.Printf("加载背景失败: %v", err)
		}
	}
	
	if cfg.Screen.TextColor != "" {
		renderer.SetTextColor(cfg.Screen.TextColor)
	}
	
	if cfg.Web.Enabled {
		webServer := web.NewServer(mon, fanCtrl, rgbCtrl, brightness, cfg, renderer)
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Web.Port)
			log.Printf("Web 服务器启动在 %s", addr)
			if err := webServer.Run(addr); err != nil {
				log.Printf("Web 服务器错误: %v", err)
			}
		}()
	}
	
	// 尝试使用I2C触摸屏，如果失败则使用SDL触摸
	touchHandler, err := ui.NewTouchHandlerWithI2C()
	if err != nil {
		log.Printf("无法打开I2C触摸屏，使用SDL触摸: %v", err)
		touchHandler = ui.NewTouchHandler()
	} else {
		log.Println("使用I2C触摸屏")
		defer touchHandler.Close()
	}
	
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	pages := []ui.Page{
		ui.NewCPUPage(mon),
		ui.NewMemoryPage(mon),
		ui.NewNetworkPage(mon),
		ui.NewDiskPage(mon),
		ui.NewFanPage(mon, fanCtrl),
		ui.NewScreenOffPage(brightness),
	}
	currentPage := 0
	log.Printf("初始化完成，共 %d 个页面，当前页面: %d", len(pages), currentPage)
	
	// 定时关屏功能
	lastTouchTime := time.Now()
	screenTimeout := time.Duration(cfg.Screen.Timeout) * time.Second
	screenOff := false
	
	// 配置重载ticker
	configReloadTicker := time.NewTicker(5 * time.Second)
	defer configReloadTicker.Stop()
	
	running := true
	for running {
		// 定期重载配置
		select {
		case <-configReloadTicker.C:
			if newCfg, err := config.Load(); err == nil {
				screenTimeout = time.Duration(newCfg.Screen.Timeout) * time.Second
			}
		default:
		}
		
		// 检查I2C触摸事件
		if touchHandler != nil {
			gesture := touchHandler.ReadI2CTouch()
			if gesture != ui.GestureNone {
				lastTouchTime = time.Now()
				if screenOff {
					// 恢复屏幕
					brightness.Set(cfg.Screen.Brightness)
					screenOff = false
				}
			}
			switch gesture {
			case ui.GestureSwipeLeft:
				oldPage := currentPage
				currentPage = (currentPage + 1) % len(pages)
				log.Printf("【页面切换】左滑: %d -> %d", oldPage, currentPage)
			case ui.GestureSwipeRight:
				oldPage := currentPage
				currentPage = (currentPage - 1 + len(pages)) % len(pages)
				log.Printf("【页面切换】右滑: %d -> %d", oldPage, currentPage)
			}
			
			// 检查点击事件
			if tap := touchHandler.ReadTap(); tap != nil {
				lastTouchTime = time.Now()
				if screenOff {
					brightness.Set(cfg.Screen.Brightness)
					screenOff = false
				} else {
					log.Printf("【点击】页面%d 坐标(%d, %d)", currentPage, tap.X, tap.Y)
					// 处理特定页面的点击
					switch p := pages[currentPage].(type) {
					case *ui.FanPage:
						p.HandleTap(tap.X, tap.Y, fanCtrl)
					case *ui.ScreenOffPage:
						p.HandleTap(tap.X, tap.Y)
						screenOff = true
					}
				}
			}
		}
		
		// 定时关屏检查
		if screenTimeout > 0 && !screenOff && time.Since(lastTouchTime) > screenTimeout {
			brightness.Set(0)
			screenOff = true
		}
		
		// 检查SDL事件（用于退出和备用触摸）
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			if _, ok := event.(*sdl.QuitEvent); ok {
				log.Println("收到退出信号")
				running = false
			}
			
			// 如果不是I2C触摸屏，使用SDL事件
			gesture := touchHandler.HandleEvent(event)
			switch gesture {
			case ui.GestureSwipeLeft:
				oldPage := currentPage
				currentPage = (currentPage + 1) % len(pages)
				log.Printf("【页面切换】SDL左滑: %d -> %d", oldPage, currentPage)
			case ui.GestureSwipeRight:
				oldPage := currentPage
				currentPage = (currentPage - 1 + len(pages)) % len(pages)
				log.Printf("【页面切换】SDL右滑: %d -> %d", oldPage, currentPage)
			}
		}
		
		select {
		case <-ticker.C:
			mon.UpdateCPU()
			mon.UpdateMemory()
			mon.UpdateNetwork()
			mon.UpdateProcesses()
			mon.UpdateDisk()
			
			renderer.DrawPage(pages[currentPage], screenOff)
		default:
			// 短暂休眠避免CPU占用过高
			time.Sleep(10 * time.Millisecond)
		}
	}
	
	cfg.Save()
}
