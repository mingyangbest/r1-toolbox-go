package main

import (
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/veandco/go-sdl2/sdl"

	"r1-toolbox/internal/config"
	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
	"r1-toolbox/internal/netinfo"
	"r1-toolbox/internal/ui"
	"r1-toolbox/internal/web"
)

// genWebToken 生成 8 位面板令牌。字符集剔除 0/O/1/I/L 等易混字符，
// 屏幕小字上抄写不会认错。
func genWebToken() (string, error) {
	const alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i, v := range b {
		b[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(b), nil
}

const (
	// 帧率两档。
	//
	// 页面滑动单独给 60fps：376px 宽的位移在 0.38s 内完成，
	// 30fps 只有 11 帧、每帧跳 34px —— 无论缓动怎么调都会"一顿一顿"。
	//
	// 🔴 天气图标已改**静态**（洋哥拍板，2026-09-27）：概览页不再有常驻
	// 动画，"animating" 只剩**数值缓动**一种含义 —— 12fps 让 0.16s 的
	// 缓动过渡至少落 2 帧，看得出是渐变而不是跳变。曾经的自适应帧率
	// 机制（animFPSFor：按动画位移速度反推帧率）随动画一起退役。
	//
	// 静止（既没有换页也没有缓动）8fps 心跳，配合跳帧机制，
	// 静态页 8 帧里 7 帧什么都不画，纯粹保触摸响应。
	fpsSlide  = 60
	fpsEase   = 12
	fpsIdle   = 8
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
	// 机箱风扇的自动调速改为跟踪**真实盘体温度**（多盘取最高）。
	// 数据源就是硬盘页用的那个 DiskMonitor —— 屏幕上看到的盘温与调速用的盘温
	// 永远同源，不会出现"界面 37°C、风扇却按 48°C 吹"这种对不上的情况。
	fanCtrl.SetHDDTempSource(func() float64 { return mon.Disk.MaxDiskTemp() })
	fanCtrl.Init()

	rgbCtrl := hardware.NewRGBController()
	rgbCtrl.SetMode(cfg.RGB.Mode, cfg.RGB.Color)

	brightness := hardware.NewBrightnessController(cfg.Hardware.BrightnessPath, 0, 96000)
	brightness.Set(cfg.Screen.Brightness)

	// ---------------- Web 面板令牌（2026-09-27 安全修复）----------------
	//
	// 🔴 修复前 /api/* 完全无鉴权且监听所有网卡，局域网任意设备可以
	// 息屏、改风扇曲线。现在：令牌为空时生成 8 位无歧义随机串
	// （去掉 0/O/1/I/L，屏幕上抄写不出错），持久化到配置、打印到
	// 启动日志、显示在屏幕「系统」页 —— 物理接触设备即授权。
	if cfg.Web.Token == "" {
		if tok, err := genWebToken(); err == nil {
			cfg.Web.Token = tok
			cfg.Save()
		} else {
			log.Printf("生成 Web 令牌失败（面板将以无令牌状态运行）: %v", err)
		}
	}
	if cfg.Web.Enabled && cfg.Web.Token != "" {
		log.Printf("Web 面板令牌: %s（屏幕「系统」页同步显示）", cfg.Web.Token)
	}

	go fanCtrl.Run()

	renderer, err := ui.NewRenderer(mon, fanCtrl, brightness, cfg.Web.Token)
	if err != nil {
		log.Fatal(err)
	}
	defer renderer.Cleanup()

	// ---------------- 外网信息（公网 IP / 天气），后台定时刷新 ----------------
	netSvc := netinfo.New()
	netSvc.Start()
	renderer.Ctx.Net = netSvc
	log.Println("外网信息服务已启动（公网 IP / 天气）")

	// ---------------- 关屏状态（超时息屏 与 界面按钮 共用一条通路）----------------
	var screenOff bool
	lastTouchTime := time.Now()

	setScreenOff := func(reason string) {
		if screenOff {
			return
		}
		brightness.Set(0)
		screenOff = true
		renderer.SetScreenOff(true)
		log.Printf("【关屏】%s", reason)
	}
	wake := func() {
		if !screenOff {
			return
		}
		brightness.Set(cfg.Screen.Brightness)
		screenOff = false
		renderer.SetScreenOff(false)
		lastTouchTime = time.Now()
		log.Println("【唤醒】触摸唤醒屏幕")
	}
	// 界面上的「关闭屏幕」按钮走这里
	renderer.Ctx.OnScreenOff = func() { setScreenOff("用户手动关屏") }

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

	// 优先 I2C 触摸屏，失败则回退 SDL 触摸
	useSDLTouch := false
	touchHandler, err := ui.NewTouchHandlerWithI2C()
	if err != nil {
		log.Printf("I2C触摸屏直连不可用（R1固件预期行为），已回退SDL触摸: %v", err)
		touchHandler = ui.NewTouchHandler()
		useSDLTouch = true
	} else {
		log.Println("使用I2C触摸屏")
		defer touchHandler.Close()
	}

	startTime := time.Now()
	screenTimeout := time.Duration(cfg.Screen.Timeout) * time.Second
	if screenTimeout <= 0 {
		log.Println("屏幕模式: 常亮（不自动息屏）")
	}

	var lastDiskUpdate time.Time
	lastDataUpdate := time.Now()

	slideDur := time.Second / fpsSlide
	idleDur := time.Second / fpsIdle
	easeDur := time.Second / fpsEase

	configReloadTicker := time.NewTicker(5 * time.Second)
	defer configReloadTicker.Stop()

	// 处理点击：派发给当前页（若支持）
	dispatchTap := func(x, y int) {
		if screenOff {
			return
		}
		if tp, ok := renderer.CurrentPage().(ui.Tappable); ok {
			tp.Tap(x, y, renderer.Ctx)
			// 点按会改页面状态（风扇模式、亮度读数等），这些不经数据采样，
			// 跳帧机制感知不到 —— 显式强制下一帧全屏重绘，反馈才不会"卡 2 秒"。
			renderer.Invalidate()
		}
	}

	log.Printf("初始化完成，共 %d 个页面", renderer.PageCount())

	// ---------------- 性能自检 ----------------
	//
	// 这块屏常亮，帧率直接等于 CPU 占用与发热，所以"每帧到底花在哪"必须
	// 是量出来的、不是猜出来的。每 5 秒打一行：实际帧率 / 单帧渲染耗时 /
	// 单帧循环耗时（不含末尾主动 sleep）。
	//
	// 判读方法：循环耗时 → 100% 说明这一档帧率已经跑满 CPU；渲染耗时占
	// 循环耗时的大头，优化点就在渲染里（全屏重绘 vs 局部重绘）。
	var perfRender, perfTotal time.Duration
	var perfFrames int
	perfSince := time.Now()
	// 把"渲染之外"的时间也量出来。之前只知道渲染占 37.6ms/s，
	// 但整个循环是 52ms/s —— 差的这 15ms 到底是事件轮询还是每秒一次
	// 的系统采样，光看渲染日志永远看不出来，必须分开量。
	var perfPoll, perfSample time.Duration
	// 采样一次要 15ms、每秒一次 = 15.5ms/s，占了忙循环的四分之一。
	// 拆到每个 Update* 才知道该优化谁。
	var perfCPU, perfMem, perfNet, perfDisk time.Duration

	running := true
	// 唤醒屏幕的那一次触摸序列要整体吞掉：手指按下时唤醒、抬起时才产生"点击"，
	// 若不吞掉，唤醒动作会顺带触发底下的按钮（例如又按到「关闭屏幕」）。
	var swallowUntil time.Time

	for running {
		frameStart := time.Now()

		// ---------------- SDL 事件 ----------------
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			if _, ok := event.(*sdl.QuitEvent); ok {
				log.Println("收到退出信号")
				running = false
				continue
			}

			// 触摸/鼠标事件是 SDL 唯一可能把指针重新显示出来的时机，这里保持关闭。
			// 先用 QUERY 查状态，只有它真的被重新打开时才打一次 ioctl。
			if v, _ := sdl.ShowCursor(sdl.QUERY); v == sdl.ENABLE {
				sdl.ShowCursor(sdl.DISABLE)
				log.Println("鼠标指针被重新打开，已再次隐藏")
			}

			gesture, tap := touchHandler.HandleEvent(event)

			// 消化唤醒序列：收到抬起事件即解除，另有 2 秒兜底
			if swallowUntil.After(frameStart) {
				switch e := event.(type) {
				case *sdl.TouchFingerEvent:
					if e.Type == sdl.FINGERUP {
						swallowUntil = time.Time{}
					}
				case *sdl.MouseButtonEvent:
					if e.Type == sdl.MOUSEBUTTONUP {
						swallowUntil = time.Time{}
					}
				}
				continue
			}

			woke := false
			if useSDLTouch {
				switch event.(type) {
				case *sdl.TouchFingerEvent, *sdl.MouseButtonEvent:
					lastTouchTime = time.Now()
					if screenOff {
						wake()
						woke = true
						swallowUntil = frameStart.Add(2 * time.Second)
					}
				}
			}
			if woke {
				continue
			}

			switch gesture {
			case ui.GestureSwipeLeft:
				renderer.SwitchPage(1)
			case ui.GestureSwipeRight:
				renderer.SwitchPage(-1)
			case ui.GestureSwipeUp:
				renderer.NextTheme()
			}

			if tap != nil {
				dispatchTap(int(tap.X), int(tap.Y))
			}
		}

		// ---------------- I2C 触摸 ----------------
		if !useSDLTouch && touchHandler != nil {
			if g := touchHandler.ReadI2CTouch(); g != ui.GestureNone {
				lastTouchTime = time.Now()
				wake()
				switch g {
				case ui.GestureSwipeLeft:
					renderer.SwitchPage(1)
				case ui.GestureSwipeRight:
					renderer.SwitchPage(-1)
				case ui.GestureSwipeUp:
					renderer.NextTheme()
				}
			}
			if tap := touchHandler.ReadTap(); tap != nil {
				lastTouchTime = time.Now()
				dispatchTap(int(tap.X), int(tap.Y))
			}
		}
		perfPoll += time.Since(frameStart)

		// ---------------- 配置热重载 ----------------
		select {
		case <-configReloadTicker.C:
			if newCfg, err := config.Load(); err == nil {
				screenTimeout = time.Duration(newCfg.Screen.Timeout) * time.Second
				cfg.Screen.Brightness = newCfg.Screen.Brightness
			}
		default:
		}

		// ---------------- 数据采样（每 2 秒）----------------
		//
		// 采样节奏 = 屏幕数字的刷新节奏。1s→2s 是一次明确的取舍：
		// 采样本身（3.4ms）与它触发的数据重绘帧（每帧 9~15ms）都减半，
		// 合计省约 12ms/s；代价是数字最多滞后 2 秒 —— 对一块状态屏
		// 无感，且显示的永远是真实采样值。历史曲线的窗口仍是 60 秒
		// （容量已同步砍半，见 monitor/system.go），标签口径不变。
		if frameStart.Sub(lastDataUpdate) >= 2*time.Second {
			lastDataUpdate = frameStart
			tSample := time.Now()
			mon.UpdateCPU()
			t1 := time.Now()
			mon.UpdateMemory()
			t2 := time.Now()
			mon.UpdateNetwork()
			t3 := time.Now()
			// 🔴 这里原本每 5 秒跑一次 mon.UpdateProcesses()，实测单次耗时 72~90ms
			// （对 390 个进程逐个读 5~6 个 procfs 文件，且 CPUPercent 会反复解析 /proc/stat），
			// 折合 14~18 ms/s，占了当时总开销的四分之一。
			// 而它算出来的 s.Processes（Top10 列表）**全程没有任何读取方** ——
			// 唯一的消费者在一个 .bak 备份目录里，是早期设计的遗留死代码。
			// 屏幕上显示的「进程数」走的是 UpdateCPU 里的 countProcesses()，与它无关。
			// 已彻底删除（连同 internal/monitor/process.go 与 gopsutil/v3/process 依赖）。
			if time.Since(lastDiskUpdate) >= 60*time.Second {
				mon.UpdateDisk()
				lastDiskUpdate = frameStart
			}
			t4 := time.Now()
			perfCPU += t1.Sub(tSample)
			perfMem += t2.Sub(t1)
			perfNet += t3.Sub(t2)
			perfDisk += t4.Sub(t3)
			perfSample += t4.Sub(tSample)
			renderer.Ctx.Now = time.Now()
			// 数字全变了 —— 通知渲染器这一帧必须整页重画。
			// 局部重绘只在"数据没动、也没有数值在缓动"时才允许。
			renderer.Ctx.BumpData()
		}

		// ---------------- 自动息屏（timeout 为 0 时常亮）----------------
		if screenTimeout > 0 && !screenOff && time.Since(lastTouchTime) > screenTimeout {
			setScreenOff("超时自动息屏")
		}

		// ---------------- 渲染 ----------------
		// Tick 放在判定之前：换页刚被触发的那一帧就要立刻切到 60fps，
		// 否则第一帧仍按静止档的 125ms 间隔走，动画开场就少掉两帧。
		renderer.Ctx.T = frameStart.Sub(startTime).Seconds()
		renderer.Tick(frameStart)
		switching := renderer.Animating()
		animating := switching || renderer.Ctx.NeedsAnim()
		renderer.Ctx.BeginFrame()
		tRender := time.Now()
		renderer.Render(renderer.Ctx)
		perfRender += time.Since(tRender)
		perfFrames++
		perfTotal += time.Since(frameStart)

		// ---------------- 帧率控制 ----------------
		//
		// 滑动 60fps（位移敏感）> 数值缓动 12fps（0.16s 过渡落 2 帧）>
		// 静止 8fps 心跳（配合跳帧，静态页基本零开销，只保触摸响应）。
		target := idleDur
		if screenOff {
			target = time.Second // 息屏时画面全黑，无需高频重绘
		} else if switching {
			target = slideDur // 滑动：最高优先级
		} else if animating {
			// 只剩数值缓动一种"动画"：12fps 让 0.16s 过渡落 2 帧
			target = easeDur
		}
		if spent := time.Since(frameStart); spent < target {
			time.Sleep(target - spent)
		}

		if perfFrames > 0 && time.Since(perfSince) >= 5*time.Second {
			el := time.Since(perfSince)
			n := float64(perfFrames)
			sec := el.Seconds()
			// 一条能直接换算成 CPU 的账：每秒各花多少毫秒。
			// 渲染 + 事件轮询 + 数据采样 ≈ 整个忙循环；剩下的就是睡眠。
			log.Printf("【性能】%.0f 帧 / %.1fs = %.1f fps | 渲染 %.1fms/s | "+
				"轮询 %.1fms/s | 采样 %.1fms/s | 忙 %.1fms/s（单核约 %.1f%%）",
				n, sec, n/sec,
				float64(perfRender.Microseconds())/1000/sec,
				float64(perfPoll.Microseconds())/1000/sec,
				float64(perfSample.Microseconds())/1000/sec,
				float64(perfTotal.Microseconds())/1000/sec,
				float64(perfTotal.Microseconds())/1000/sec/10)
			log.Printf("【采样分解】(ms/s) CPU %.1f | 内存 %.1f | 网络 %.1f | 磁盘 %.1f",
				float64(perfCPU.Microseconds())/1000/sec,
				float64(perfMem.Microseconds())/1000/sec,
				float64(perfNet.Microseconds())/1000/sec,
				float64(perfDisk.Microseconds())/1000/sec)
			perfFrames, perfRender, perfTotal = 0, 0, 0
			perfPoll, perfSample = 0, 0
			perfCPU, perfMem, perfNet, perfDisk = 0, 0, 0, 0
			perfSince = time.Now()
		}
	}

	cfg.Save()
}
