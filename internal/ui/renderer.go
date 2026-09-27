package ui

import (
	"fmt"
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"

	"r1-toolbox/internal/config"
	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
)

// ============================================================
//  渲染器：Canvas 自绘 → 单次纹理上传
// ============================================================

type Renderer struct {
	window *sdl.Window
	ren    *sdl.Renderer
	tex    *sdl.Texture

	Canvas *Canvas
	Fonts  *FontSet
	Ctx    *DrawCtx

	theme *Theme
	pages []Page
	cur   int

	// 页面切换动画
	animFrom int
	animTo   int
	animDir  int
	animT    float64   // 0..1 的进度
	animAt   time.Time // 动画起点（绝对时间）
	animN    int       // 本次切换实际画了多少帧（用于排查滑动流畅度）

	// 换页用的位图快照。
	//
	// 滑动的 0.38 秒里，两页的内容**一个像素都不会变**（数据每秒才采样一次），
	// 逐帧重画它们纯粹是浪费：实测重画一页约 19ms、两页 38ms，帧率被钉死在 26fps，
	// 而 60fps 的预算是 16.6ms。所以滑动期间改成搬两张位图（约 1ms）。
	snapFrom *Sprite // 滑出的那一页（换页瞬间截下的当前画面）
	snapTo   *Sprite // 滑入的那一页（滑动的第一帧渲染一次后截下）

	screenOff bool

	// ---- 局部重绘（见 damageRects）----
	//
	// needFull：画布内容不可信（刚启动 / 刚换页 / 刚切主题 / 刚唤醒），
	// 必须先整页重建一次。
	needFull     bool
	lastGen      uint64 // 上一帧的数据代数
	lastEasing   bool   // 上一帧是否有数值在缓动
	lastClockMin int    // 上一帧顶栏时钟的分钟数
	// lastWx 天气快照指纹。天气卡不在 DataRects 的重绘范围内，全靠这道
	// 校验兜底：快照一变（温度/描述/湿度……）立即退全屏重画天气卡。
	lastWx string

	// 局部重绘这一帧实际动过的矩形，用于"只上传这一块"。
	uploadRects []Rect

	// 渲染分段自检。这块屏常亮，帧时间直接换算成 CPU 与发热 ——
	// 优化前必须先知道"页面绘制 / 顶栏 / 纹理上传"各占多少，
	// 否则很容易去优化一个根本不是瓶颈的部分。
	//
	// 再按"局部帧 / 全屏帧"分开统计：两者的成本差一个数量级
	// （局部帧只重画 140×140 的天气图标区，全屏帧要画整页）。
	// 只看总数会得出"单帧只要 2ms"这种没有意义的平均值 ——
	// CPU 是 Σ(每帧耗时)，不是 帧率 × 平均耗时，
	// 而全屏帧的次数是由**数据更新（每秒一次）与它引发的缓动**决定的，
	// 跟帧率无关。所以这两条必须分开看才知道该优化哪一边。
	perfDraw, perfBar, perfUp             time.Duration
	perfLocalDraw, perfFullDraw           time.Duration
	perfLocalUp, perfFullUp               time.Duration
	perfN, perfLocalN, perfFullN          int
	perfSkipN                             int // 跳帧数：内容没变、画布原样可信，整帧不画不上传
	perfFullReason                        [5]int
	perfSince                             time.Time
}

const (
	// 页面切换时长。0.30s 太赶，在 376px 宽度上像"闪"了一下；
	// 0.38s 配缓入缓出，才读得出"推过去"这个动作。
	sliceDur = 0.38
	clipTop  = 96
	clipBot  = 896
)

// pageAccents 每一页的语义色 —— 用于顶栏页名色标与底部页签。
//
// 顺序与 r.pages 一一对应，五个色相互不相同：
// 看到青色页签就是概览、橙色是性能、紫色是硬盘、蓝色是网络、黄色是系统。
var pageAccents = []RGBA{
	P.Mem,     // 概览
	P.Cpu,     // CPU / 内存
	P.DiskSSD, // 硬盘
	P.NetDown, // 网络
	P.Fan,     // 系统
}

// pageAccent 取第 i 页的语义色（越界返回零值，调用方会退化为中性）
func (r *Renderer) pageAccent(i int) RGBA {
	if i < 0 || i >= len(pageAccents) {
		return RGBA{}
	}
	return pageAccents[i]
}

func NewRenderer(mon *monitor.SystemMonitor, fan *hardware.FanController, br *hardware.BrightnessController, webToken string) (*Renderer, error) {
	// 这台机器的 SDL 实际由 systemd 指定走 KMSDRM（SDL_VIDEODRIVER=kmsdrm），
	// 因此下面的 SDL_FBDEV 并不生效，保留只是为了兼容可能存在的 fbdev 回退。
	os.Setenv("SDL_FBDEV", "/dev/fb0")

	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		return nil, fmt.Errorf("SDL 初始化失败: %v", err)
	}
	if err := ttf.Init(); err != nil {
		return nil, fmt.Errorf("TTF 初始化失败: %v", err)
	}

	log.Println("尝试创建 SDL 窗口...")
	window, err := sdl.CreateWindow("R1 Toolbox", 0, 0,
		config.ScreenWidth, config.ScreenHeight, sdl.WINDOW_SHOWN)
	if err != nil {
		return nil, fmt.Errorf("创建窗口失败: %v", err)
	}

	log.Println("创建渲染器...")
	ren, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_SOFTWARE)
	if err != nil {
		return nil, fmt.Errorf("创建渲染器失败: %v", err)
	}
	ren.SetLogicalSize(config.ScreenWidth, config.ScreenHeight)

	tex, err := ren.CreateTexture(sdl.PIXELFORMAT_ABGR8888, sdl.TEXTUREACCESS_STREAMING,
		config.ScreenWidth, config.ScreenHeight)
	if err != nil {
		return nil, fmt.Errorf("创建纹理失败: %v", err)
	}
	tex.SetBlendMode(sdl.BLENDMODE_NONE)

	// 关掉 SDL 自带的鼠标指针。
	//
	// 这块屏是纯触摸的，但 SDL 的 KMSDRM 后端会创建一个 **硬件光标层** 并默认
	// 显示一个箭头；同时 evdev 把触摸屏也当成鼠标设备注册进 SDL 鼠标子系统，
	// 于是没人去挪它，箭头就一直钉在屏幕左上角（而且它画在 DRM cursor plane 上，
	// 不在主 framebuffer 里，所以直接 dd /dev/fb0 是看不到的）。
	if _, err := sdl.ShowCursor(sdl.DISABLE); err != nil {
		log.Printf("隐藏鼠标指针失败: %v", err)
	} else {
		log.Println("已隐藏鼠标指针")
	}

	fs := NewFontSet()
	if fs.Path() == "" {
		log.Println("警告: 未找到任何可用字体，文字将不显示")
	} else {
		log.Printf("成功加载字体: %s", fs.Path())
		log.Printf("拉丁字体: %s", fs.LatinPath())
	}

	r := &Renderer{
		window: window,
		ren:    ren,
		tex:    tex,
		Canvas: NewCanvas(config.ScreenWidth, config.ScreenHeight),
		Fonts:  fs,
		theme:  themeInk,
		// 第一帧必须整页重建：画布里还没有任何可信内容
		needFull:     true,
		lastClockMin: -1,
	}
	r.Ctx = NewDrawCtx(fs)
	r.Ctx.Theme = r.theme
	r.Ctx.Mon = mon
	r.Ctx.Fan = fan
	r.Ctx.Bright = br

	// 页面顺序（洋哥指定）：概览 → CPU/内存 → 硬盘 → 网络 → 系统
	r.pages = []Page{
		NewOverviewPage(mon),
		NewPerfPage(mon),
		NewDiskPage(mon),
		NewNetworkPage(mon),
		NewSystemPage(mon, fan, br, webToken),
	}

	r.rebuildBackground()
	log.Println("SDL2 渲染器初始化成功")
	return r, nil
}

// rebuildBackground 重建静态背景底图
func (r *Renderer) rebuildBackground() {
	c := r.Canvas
	c.SetOffset(0, 0)
	c.ClearClip()
	c.SetGlobalAlpha(1)
	c.Clear(r.theme.BgBot)
	DrawBackground(c, r.theme)
	c.SetBase()
}

// ------------------------------------------------------------
//  页面管理
// ------------------------------------------------------------

func (r *Renderer) PageCount() int  { return len(r.pages) }
func (r *Renderer) CurrentIndex() int { return r.cur }
func (r *Renderer) CurrentPage() Page { return r.pages[r.cur] }
func (r *Renderer) PageAt(i int) Page {
	if i < 0 || i >= len(r.pages) {
		return nil
	}
	return r.pages[i]
}

// SwitchPage dir>0 下一页（左滑），dir<0 上一页（右滑）
func (r *Renderer) SwitchPage(dir int) {
	if r.animT > 0 && r.animT < 1 {
		return // 动画进行中，忽略
	}
	n := len(r.pages)
	if n == 0 {
		return
	}
	r.animFrom = r.cur
	if dir > 0 {
		r.cur = (r.cur + 1) % n
	} else {
		r.cur = (r.cur - 1 + n) % n
	}
	r.animTo = r.cur
	r.animDir = dir
	r.animAt = time.Now()
	r.animT = 0.0001
	// 此刻画布上还是"旧页的完整一帧"（上一步渲染留下的），直接截下来复用。
	r.snapFrom = r.Canvas.Snapshot(0, clipTop, r.Canvas.W, clipBot-clipTop)
	r.snapTo = nil
	log.Printf("【页面切换】%d -> %d (dir=%d)", r.animFrom, r.animTo, dir)
}

// Animating 是否正在切换动画
func (r *Renderer) Animating() bool { return r.animT > 0 && r.animT < 1 }

// Tick 推进切换动画。
//
// 🔴 这里必须用**绝对时间**，不能拿"上一帧耗时"来累加。
// 静止时帧率是 8fps，也就是主循环末尾会 sleep 0.125 秒 —— 而换页是在这一帧
// 的事件处理里被触发的，那一帧的"上一帧耗时"整整 0.125s，等于把动画
// 一口气推进了 1/3。结果是 0.38 秒的滑动只来得及画 3 帧，无论缓动怎么调
// 都是"闪一下"。改成按 animAt 到现在的真实间隔计算进度后，
// 动画进度与帧率解耦：帧率不够就是掉帧，但时长一定是对的。
func (r *Renderer) Tick(now time.Time) {
	if r.animAt.IsZero() {
		return
	}
	t := now.Sub(r.animAt).Seconds() / sliceDur
	if t >= 1 {
		t = 1
		// 滑动刚结束：画布上现在是"两张页面各占一半"的拼接位图，
		// 既不是旧页也不是新页，必须整页重建一次才能继续走局部重绘。
		if r.animT < 1 {
			r.needFull = true
		}
	}
	r.animT = t
}

// SetTheme 切换主题并重建背景
func (r *Renderer) SetTheme(name string) {
	r.theme = ThemeByName(name)
	r.Ctx.Theme = r.theme
	r.rebuildBackground()
	// 背景底图换了，画布上现有的像素全部作废
	r.needFull = true
	log.Printf("【主题】切换为 %s", r.theme.Name)
}

func (r *Renderer) Theme() *Theme { return r.theme }

// NextTheme 循环切换主题
func (r *Renderer) NextTheme() string {
	names := ThemeNames()
	cur := r.theme.Name
	for i, n := range names {
		if n == cur {
			next := names[(i+1)%len(names)]
			r.SetTheme(next)
			return next
		}
	}
	r.SetTheme(names[0])
	return names[0]
}

// SetScreenOff 设置熄屏状态
func (r *Renderer) SetScreenOff(off bool) {
	r.screenOff = off
	// 关屏时画布被整屏涂黑、唤醒后内容全不可信
	r.needFull = true
}

// ------------------------------------------------------------
//  绘制
// ------------------------------------------------------------

// Rect 屏幕矩形（像素坐标）
type Rect struct{ X, Y, W, H int }

// Damaged 可选接口：页面声明"本帧只有这些矩形在动"。
//
// 首页的常驻动画只有天气图标那一小块（96×96），但旧实现每帧都要把整页
// 重画一遍 —— 卡片、标签、发丝线、所有数值，一秒二十次。有了这个接口，
// 不动的部分就可以原样留在画布上，只重画真正会变的区域。
//
// 约定：返回的矩形之外，页面必须与上一帧**逐像素完全相同**，否则会出现
// 残留。所以实现方要非常保守 —— 拿不准就返回 nil 走全屏。
type Damaged interface {
	DamagedRects(d *DrawCtx) []Rect
}

func (r *Renderer) Render(d *DrawCtx) {
	c := r.Canvas
	tDraw := time.Now()

	if r.screenOff {
		c.Reset()
		c.SetOffset(0, 0)
		c.ClearClip()
		c.SetGlobalAlpha(1)
		c.Clear(RGBA{0, 0, 0, 255})
		r.upload()
		return
	}

	local := false
	if r.Animating() {
		r.animN++
		// 滑入的那一页只在**第一帧**渲染一次，之后全程搬位图。
		if r.snapTo == nil {
			c.RestoreRegion(0, clipTop, c.W, clipBot-clipTop)
			c.SetOffset(0, 0)
			c.SetClip(0, clipTop, c.W, clipBot-clipTop)
			r.pages[r.animTo].Draw(c, d)
			c.ClearClip()
			r.snapTo = c.Snapshot(0, clipTop, c.W, clipBot-clipTop)
		}

		e := easeInOutCubic(r.animT)
		w := c.W
		// 两页的可见列区间必须**严丝合缝地拼满整屏**：
		// 旧页滑出 k 像素，新页就从 w-k 处开始滑入，接口正好落在 w-k。
		var dxFrom, dxTo int
		if r.animDir > 0 {
			dxFrom = -int(float64(w) * e)     // 旧页向左滑出
			dxTo = int(float64(w) * (1 - e))  // 新页自右侧滑入
		} else {
			dxFrom = int(float64(w) * e)      // 旧页向右滑出
			dxTo = -int(float64(w) * (1 - e)) // 新页自左侧滑入
		}
		// 🔴 这里必须整屏恢复底图，不能只恢复页面区。
		//
		// 顶栏的页名是"旧名淡出 → 新名淡入"两段画的，中间 alpha 接近 0，
		// 如果底下没有先擦掉旧名，两个名字就会叠成"CPU / 概览"这种糊影
		// （实测抓换页中段时就是这个现象）。页签同理。
		// Reset 只是 1.44MB 的 memcpy，约 0.1ms，不必省。
		c.Reset()
		c.BlitOpaque(r.snapFrom, dxFrom, clipTop)
		c.BlitOpaque(r.snapTo, dxTo, clipTop)
	} else if rects, mode := r.planFrame(d); mode == frameLocal {
		// ---- 局部重绘 ----
		p := r.pages[r.cur]
		r.uploadRects = r.uploadRects[:0]
		for _, rc := range rects {
			c.RestoreRegion(rc.X, rc.Y, rc.W, rc.H)
			c.SetGlobalAlpha(1)
			c.SetOffset(0, 0)
			c.SetClip(rc.X, rc.Y, rc.W, rc.H)
			p.Draw(c, d)
			r.uploadRects = append(r.uploadRects, rc)
		}
		c.SetGlobalAlpha(1)
		c.SetOffset(0, 0)
		c.ClearClip()
		local = true
	} else if mode == frameSkip {
		// ---- 跳帧：画布内容与"本该画出来的样子"逐像素一致，什么都不用做 ----
		//
		// 条件由 planFrame 保证：无常驻动画的页 + 数据代数没变 + 上一帧
		// 没有缓动 + 顶栏分钟没跨。此时重绘是纯粹的浪费 —— 硬盘页、
		// 系统页这类静态页在 8fps 心跳下 8 帧里 7 帧落在这里，每帧只花
		// 一次函数调用的钱。点按/换页/主题切换都会先置 needFull 或改
		// 数据代数，不会被困在这个分支里（风扇转速这类实时读数最多
		// 滞后一个采样周期 2 秒）。
		r.perfSkipN++
		return
	} else {
		c.Reset()
		r.drawPageBody(r.pages[r.cur], d, 1, 0)
		if r.animN > 0 {
			// 一条可核对的滑动流畅度记录：0.38s / 帧数 ≈ 实际帧率
			log.Printf("【换页】%d→%d 共 %d 帧 (%.2fs ≈ %.0ffps)",
				r.animFrom, r.animTo, r.animN, sliceDur,
				float64(r.animN)/sliceDur)
			r.animN = 0
			r.snapFrom = nil
			r.snapTo = nil
		}
		r.needFull = false
		// 全屏重画过，天气卡已是最新 —— 刷新指纹，防止下一帧误判"天气变了"。
		r.lastWx = wxStampOf(d)
	}

	// 顶栏与页签不参与滑动，但同样不能"啪"地跳到新页 —— 让它们随进度过渡。
	dDraw := time.Since(tDraw)
	r.perfDraw += dDraw
	if local {
		r.perfLocalDraw += dDraw
	} else {
		r.perfFullDraw += dDraw
	}
	tBar := time.Now()
	c.SetGlobalAlpha(1)
	c.SetOffset(0, 0)
	c.ClearClip()

	if r.Animating() {
		DrawTopBarClock(c, r.Fonts, r.theme, d.Now)
		nameCY := TopBarNameCY(r.Fonts, d.Now)
		e := easeInOutCubic(r.animT)
		// 页名：旧名先淡出、新名后淡入，且**不重叠** ——
		// 两个字在同一位置交叉淡化会叠出一团糊影，宁可中间空一下。
		if e < 0.5 {
			c.SetGlobalAlpha(1 - e*2)
			DrawTopBarName(c, r.Fonts, r.theme, nameCY, pageNameOf(r.pages[r.animFrom]),
				r.pageAccent(r.animFrom))
		} else {
			c.SetGlobalAlpha((e - 0.5) * 2)
			DrawTopBarName(c, r.Fonts, r.theme, nameCY, pageNameOf(r.pages[r.animTo]),
				r.pageAccent(r.animTo))
		}
		c.SetGlobalAlpha(1)
		DrawDotsT(c, pageAccents, r.animFrom, r.animTo, e, 918)
	} else {
		// 局部重绘时顶栏只有"分钟"会变 —— 秒不变、页名不变、页签不变。
		// 拿分钟做判据：没跨分钟就整条跳过，跨了才补画那一条。
		minute := d.Now.Minute()
		if !local || minute != r.lastClockMin {
			if local {
				c.RestoreRegion(0, 0, c.W, clipTop)
				c.SetClip(0, 0, c.W, clipTop)
				r.uploadRects = append(r.uploadRects, Rect{0, 0, c.W, clipTop})
			}
			DrawTopBarClock(c, r.Fonts, r.theme, d.Now)
			DrawTopBarName(c, r.Fonts, r.theme, TopBarNameCY(r.Fonts, d.Now),
				pageNameOf(r.pages[r.cur]), r.pageAccent(r.cur))
			c.ClearClip()
			r.lastClockMin = minute
		}
		if !local {
			acc := pageAccents
			if len(acc) > len(r.pages) {
				acc = acc[:len(r.pages)]
			}
			DrawDots(c, acc, r.cur, 918)
		}
	}

	r.perfBar += time.Since(tBar)
	tUp := time.Now()
	if local && len(r.uploadRects) > 0 {
		r.uploadPartial(r.uploadRects)
	} else {
		r.upload()
	}
	dUp := time.Since(tUp)
	r.perfUp += dUp
	if local {
		r.perfLocalUp += dUp
	} else {
		r.perfFullUp += dUp
	}

	// 记录本帧状态，供下一帧判断能否走局部重绘
	r.lastGen = d.DataGen()
	r.lastEasing = d.Easing()

	r.perfN++
	if local {
		r.perfLocalN++
	} else {
		r.perfFullN++
	}
	if r.perfSince.IsZero() {
		r.perfSince = time.Now()
	}
	if r.perfN > 0 && time.Since(r.perfSince) >= 5*time.Second {
		n := float64(r.perfN)
		el := time.Since(r.perfSince).Seconds()
		per := func(d time.Duration, cnt int) float64 {
			if cnt == 0 {
				return 0
			}
			return float64(d.Microseconds()) / 1000 / float64(cnt)
		}
		// 一条能直接换算成 CPU 的账：每秒各花多少毫秒。
		msLocal := float64(r.perfLocalDraw.Microseconds()) / 1000 / el
		msFull := float64(r.perfFullDraw.Microseconds()) / 1000 / el
		msUp := float64(r.perfUp.Microseconds()) / 1000 / el
		log.Printf("【渲染分解】%.0f 帧（局部 %d / 全屏 %d / 跳过 %d）= %.1f+%.1f fps | "+
			"局部帧 %.2fms/帧 共 %.1fms/s | 全屏帧 %.2fms/帧 共 %.1fms/s | 顶栏 %.1fms/s | 上传 %.1fms/s | 全屏出口 needFull %d 天气 %d 数据 %d 空矩形 %d 分钟 %d",
			n, r.perfLocalN, r.perfFullN, r.perfSkipN,
			float64(r.perfLocalN)/el, float64(r.perfFullN)/el,
			per(r.perfLocalDraw, r.perfLocalN), msLocal,
			per(r.perfFullDraw, r.perfFullN), msFull,
			float64(r.perfBar.Microseconds())/1000/el, msUp,
			r.perfFullReason[0], r.perfFullReason[1], r.perfFullReason[2], r.perfFullReason[3], r.perfFullReason[4])
		r.perfN, r.perfLocalN, r.perfFullN = 0, 0, 0
		r.perfFullReason = [5]int{}
		r.perfDraw, r.perfLocalDraw, r.perfFullDraw = 0, 0, 0
		r.perfBar, r.perfUp, r.perfLocalUp, r.perfFullUp = 0, 0, 0, 0
		r.perfSince = time.Now()
	}
}

// 帧决策结果：跳帧（画布已是对的）/ 局部 / 全屏。
const (
	frameSkip = iota
	frameLocal
	frameFull
)

// DataDamaged 可选接口：页面声明"数据采样帧只重画这几张卡"。
//
// 数据代数一变，页面上散落的数字全要刷新 —— 旧实现为此每秒退一次全屏，
// 实测全屏帧 14.5ms、含上传约每秒吃掉 58ms/s，占了整个 CPU 预算的一半。
// 概览页借此把数据帧的重绘面积压到三张数据卡（天气卡除外，见 planFrame）。
// 没实现本接口的页退回全屏 —— 它们有跳帧兜底，2 秒才画 2 帧，不构成瓶颈。
type DataDamaged interface {
	DataRects(d *DrawCtx) []Rect
}

// wxStampOf 天气快照指纹：天气卡上显示的每一个字段都参与。
// 任何一个变了都意味着"天气卡不再是上一帧的样子"，必须整卡重画。
func wxStampOf(d *DrawCtx) string {
	w := d.WeatherNow()
	if !w.OK {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%.1f|%.1f|%.1f|%d",
		w.Kind, w.Desc, w.City, w.TempC, w.MinC, w.MaxC, w.Humidity)
}

// planFrame 静止帧的三选一决策：跳帧 / 局部 / 全屏。
//
// 判定顺序（从最便宜到最贵）：
//  1. needFull / 换页尾帧：画布不可信 → 全屏；
//  2. 天气快照变了：天气卡不在任何 DataRects 里 → 全屏（一次）；
//  3. 数据代数变了或上帧在缓动：页面实现了 DataDamaged 就局部重画数据卡，
//     否则全屏；
//  4. 有常驻动画的页（概览）：图标矩形局部重绘；
//  5. 其余静态页：内容没变 → 跳帧，一次绘制都不做。
//
// 🔴 局部重绘的约定不变：返回矩形之外，页面必须与上一帧逐像素相同。
// 拿不准就返回 nil 走全屏 —— 宁多画，不留残影。
func (r *Renderer) planFrame(d *DrawCtx) ([]Rect, int) {
	if r.needFull || r.animN > 0 {
		r.perfFullReason[0]++
		return nil, frameFull
	}
	if ws := wxStampOf(d); ws != r.lastWx {
		// 只刷新指纹不置 needFull：本帧全屏重画天气卡已经足够，
		// 下一帧起恢复正常节奏。
		r.lastWx = ws
		r.perfFullReason[1]++
		return nil, frameFull
	}
	if d.DataGen() != r.lastGen || r.lastEasing {
		if dd, ok := r.pages[r.cur].(DataDamaged); ok {
			if rc := dd.DataRects(d); len(rc) > 0 {
				return rc, frameLocal
			}
		}
		r.perfFullReason[2]++
		return nil, frameFull
	}
	if p, ok := r.pages[r.cur].(Damaged); ok {
		if rc := p.DamagedRects(d); len(rc) > 0 {
			return rc, frameLocal
		}
		r.perfFullReason[3]++
		return nil, frameFull
	}
	// 无常驻动画的页：数据没动、缓动已结束、分钟没跨 —— 画布就是对的。
	if d.Now.Minute() != r.lastClockMin {
		r.perfFullReason[4]++
		return nil, frameFull // 顶栏时钟要跨分钟，走一次全屏把顶栏带上
	}
	return nil, frameSkip
}

// Invalidate 强制下一帧全屏重绘。点按类交互（风扇调速、亮度）改变页面
// 状态但不经过数据采样，跳帧机制感知不到 —— 点完必须显式叫一声。
func (r *Renderer) Invalidate() { r.needFull = true }

// pageNameOf 取页名（未实现 Named 的页返回空串）
func pageNameOf(p Page) string {
	if p == nil {
		return ""
	}
	if n, ok := p.(Named); ok {
		return n.PageName()
	}
	return ""
}

func (r *Renderer) drawPageBody(p Page, d *DrawCtx, alpha float64, dx int) {
	if p == nil || alpha <= 0.01 {
		return
	}
	c := r.Canvas
	c.SetGlobalAlpha(alpha)
	c.SetOffset(dx, 0)
	c.SetClip(0, clipTop, c.W, clipBot-clipTop)
	p.Draw(c, d)
	c.SetGlobalAlpha(1)
	c.SetOffset(0, 0)
	c.ClearClip()
}

func (r *Renderer) upload() {
	if err := r.tex.Update(nil, unsafe.Pointer(&r.Canvas.Pix[0]), r.Canvas.W*4); err != nil {
		log.Printf("纹理上传失败: %v", err)
		return
	}
	r.ren.Copy(r.tex, nil, nil)
	r.ren.Present()
}

// uploadPartial 只把给定矩形上传并呈现。
//
// 局部重绘这一帧动过的像素可能只有 140×140，却仍要按整屏
// （376×960×4 ≈ 1.4MB）上传一次 —— 实测上传 1.4ms，是局部重绘之后
// 渲染里最大的单项。按矩形上传后源数据量降到千分之几。
//
// 任意一块出错就整体退回整屏上传：局部刷新一旦缺块，画面会永久残留，
// 宁可多花一次整屏的时间也不能让画面出错。
func (r *Renderer) uploadPartial(rects []Rect) {
	c := r.Canvas
	pitch := c.W * 4
	done := false
	for _, rc := range rects {
		x, y, w, h := rc.X, rc.Y, rc.W, rc.H
		if x < 0 {
			w += x
			x = 0
		}
		if y < 0 {
			h += y
			y = 0
		}
		if x+w > c.W {
			w = c.W - x
		}
		if y+h > c.H {
			h = c.H - y
		}
		if w <= 0 || h <= 0 {
			continue
		}
		sr := sdl.Rect{X: int32(x), Y: int32(y), W: int32(w), H: int32(h)}
		if err := r.tex.Update(&sr, unsafe.Pointer(&c.Pix[(y*c.W+x)<<2]), pitch); err != nil {
			log.Printf("局部纹理上传失败，退回整屏: %v", err)
			r.upload()
			return
		}
		if err := r.ren.Copy(r.tex, &sr, &sr); err != nil {
			log.Printf("局部呈现失败，退回整屏: %v", err)
			r.upload()
			return
		}
		done = true
	}
	if !done {
		r.upload()
		return
	}
	r.ren.Present()
}

func easeOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	u := 1 - t
	return 1 - u*u*u
}

// easeInOutCubic 页面切换用的缓动。
//
// 换掉的是 easeOutCubic —— 它的**起始速度最大、末端速度归零**，
// 也就是"猛甩过去再撞墙停住"。再加 0.3 秒的时长，就成了"生硬"本身。
// 切换是一个"推"的动作，两端都该有加速与减速。
func easeInOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
}

func (r *Renderer) Cleanup() {
	if r.Fonts != nil {
		r.Fonts.Close()
	}
	if r.tex != nil {
		r.tex.Destroy()
	}
	if r.ren != nil {
		r.ren.Destroy()
	}
	if r.window != nil {
		r.window.Destroy()
	}
	ttf.Quit()
	sdl.Quit()
}

// ------------------------------------------------------------
//  兼容旧 Web 控制面板的接口
// ------------------------------------------------------------

// SetTextColor 兼容旧接口（新 UI 使用主题配色，此处仅记录）
func (r *Renderer) SetTextColor(hex string) {}

// LoadBackground 兼容旧接口（新 UI 使用内置渐变背景）
func (r *Renderer) LoadBackground(path string) error { return nil }
