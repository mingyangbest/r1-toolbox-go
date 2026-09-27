package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
	"r1-toolbox/internal/netinfo"
)

// ============================================================
//  绘制上下文
// ============================================================

type DrawCtx struct {
	Theme     *Theme
	Fonts     *FontSet
	Mon       *monitor.SystemMonitor
	Fan       *hardware.FanController
	Bright    *hardware.BrightnessController
	Net       *netinfo.Service // 公网 IP / 天气（可能为 nil）
	Now       time.Time
	T         float64 // 启动至今秒数（仅用于动画相位）
	StartTime time.Time
	Host      string

	// OnScreenOff 由主程序注入：请求立即关屏（并可被触摸唤醒）
	OnScreenOff func()

	anim  map[string]*animState
	dirty bool
	// easing 与 dirty 分开：dirty 表示"这一帧画面还会变"（含常驻动画），
	// easing 只表示"有数值正在缓动插值" —— 后者会让一大片区域每帧都变，
	// 是局部重绘的否决条件，前者不是。
	easing bool
	// dataGen 每完成一次数据采样自增。数字类内容散落在整页，
	// 代数一变就整页重画，比逐块去猜哪里变了更简单也更可靠。
	dataGen uint64

}

// BumpData 数据采样完成后调用，宣告"屏幕上的数字全部需要刷新"
func (d *DrawCtx) BumpData() { d.dataGen++ }

// DataGen 当前数据代数
func (d *DrawCtx) DataGen() uint64 { return d.dataGen }

// Easing 本帧是否有数值在缓动
func (d *DrawCtx) Easing() bool { return d.easing }

func NewDrawCtx(fs *FontSet) *DrawCtx {
	host, _ := os.Hostname()
	return &DrawCtx{
		Fonts:     fs,
		Theme:     themeInk,
		anim:      map[string]*animState{},
		Now:       time.Now(),
		StartTime: time.Now(),
		Host:      host,
	}
}

// animState 一个缓动键的过渡状态。
//
// 🔴 旧实现是"每帧逼近剩余差值的 rate 倍"，也就是**按帧数收敛**：
// 数据每秒变一次，要 log(1/0.5)(D/0.2) 帧才收敛，D≈5.6 时约 5 帧。
// 这带来两个后果，都不可接受：
//
//  1. 缓动期间必须整页重画，于是"每秒 1 次数据更新"被放大成"每秒 5 次全屏重绘"，
//     而**降帧率根本不会减少它**（帧数没变，只是拉长了墙钟时间），
//     降帧率反而让数字的过渡拖到 0.33 秒 —— 更假、更慢。
//  2. 屏幕上显示的是"若干个采样周期前的插值"，对一个数据必须真实的仪表盘本身就是错的。
//
// 改成按**绝对时间**收敛后，过渡时长是固定的（easeDur 秒），
// 代价 = easeDur × 帧率，于是降帧率能等比例省下这部分开销，
// 而且任何帧率下过渡都是同样的 0.16 秒 —— 语义稳定，可预测。
type animState struct {
	cur    float64
	from   float64
	to     float64
	t0     float64 // 过渡起点（秒，取自 DrawCtx.T）
	active bool
}

// easeDur 数值过渡时长（秒）。取 0.16s：短到不会读出"滞后"，
// 又长到在最低 12fps 下也能落到 2 帧以上、看得出是渐变而不是跳变。
const easeDur = 0.16

// Ease 把数值平滑逼近目标值。
//
// 返回的永远是"当前应当显示的值"；过渡结束后逐位等于 target，
// 不留任何残差 —— 这点很重要，否则数字会永远差最后一点点，
// 而"数据必须真实"是这块屏的第一原则。
func (d *DrawCtx) Ease(key string, target float64) float64 {
	st := d.anim[key]
	if st == nil {
		// 首次出现：直接就位，不做无意义的"从 0 涨上来"
		st = &animState{cur: target, from: target, to: target, t0: d.T}
		d.anim[key] = st
		return target
	}
	if target != st.to {
		// 目标变了：从**当前显示值**重新起步（而不是从上一个目标），
		// 这样连续两次更新之间的过渡不会被拉长成两段。
		st.from = st.cur
		st.to = target
		st.t0 = d.T
	}
	u := (d.T - st.t0) / easeDur
	if u >= 1 || u < 0 {
		st.cur = st.to
		st.active = false
		return st.cur
	}
	st.cur = st.from + (st.to-st.from)*easeOutCubic(u)
	st.active = true
	d.dirty = true
	d.easing = true
	return st.cur
}

// BeginFrame 重置本帧动画标记
func (d *DrawCtx) BeginFrame() {
	d.dirty = false
	d.easing = false
}

// NeedsAnim 本帧是否有动画在进行（用于自适应降帧）
func (d *DrawCtx) NeedsAnim() bool { return d.dirty }

// KeepAnim 声明本页需要持续重绘（有常驻动画，例如首页天气）。
// 只在首页调用，其他页仍走 8fps 静止档以省 CPU。
func (d *DrawCtx) KeepAnim() { d.dirty = true }

// WeatherNow 取天气快照（无服务时返回零值）
func (d *DrawCtx) WeatherNow() netinfo.Weather {
	if d.Net == nil {
		return netinfo.Weather{}
	}
	return d.Net.Weather()
}

// ------------------------------------------------------------
//  页面接口
// ------------------------------------------------------------

type Page interface {
	Draw(c *Canvas, d *DrawCtx)
}

// Named 可选实现：返回页面名（顶栏中间显示）
type Named interface {
	PageName() string
}

// Tappable 可选实现：接收点击
type Tappable interface {
	Tap(x, y int, d *DrawCtx)
}

// 曲线量程档位：量化到固定档，既有起伏又不会频繁抖动
var historySteps = []float64{20, 30, 40, 50, 60, 70, 80, 90, 100}

// autoTop 依据历史峰值挑一个留 20% 余量的量程档
func autoTop(data []float64, floor float64) float64 {
	mx := floor
	for _, v := range data {
		if v > mx {
			mx = v
		}
	}
	for _, s := range historySteps {
		if mx <= s*0.8 {
			return s
		}
	}
	return 100
}

// formatUptime 把系统开机至今的秒数写成中文时长。
//
// 注意：这里用的是 /proc/uptime（系统运行时长），
// 不是本进程的启动时间 —— 否则一重启服务就显示「0时0分」。
func formatUptime(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%d天%d时", d, h)
	case h > 0:
		return fmt.Sprintf("%d时%d分", h, m)
	default:
		return fmt.Sprintf("%d分", m)
	}
}

// fmtBytes 人类可读容量
func fmtBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/MB)
	default:
		return fmt.Sprintf("%.0f KB", float64(b)/KB)
	}
}

// ============================================================
//  1. 概览页
//
//  版面（洋哥指定）：
//    上  = 天气（彩色 + 动画）
//    中  = CPU / 内存 负载 + 上传下载速率
//    下  = 系统信息（主机名 / 局域网 / 公网 IP / 已运行）
//  注意：按洋哥要求，「系统负载」不再出现在本页
// ============================================================

type OverviewPage struct{ mon *monitor.SystemMonitor }

func NewOverviewPage(mon *monitor.SystemMonitor) *OverviewPage {
	return &OverviewPage{mon: mon}
}

func (p *OverviewPage) PageName() string { return "概览" }

// DataRects 数据采样帧的"只重画这几张卡"声明。
//
// 每次采样（2 秒）变化的只有三张数据卡：负载（双环）、网络（速率+曲线）、
// 系统信息（已运行秒数）。天气卡的数据 10 分钟才变一次，且天气快照一变
// 渲染器会自动退全屏（见 planFrame 的天气戳校验），所以这里可以放心地
// 把天气卡排除在外 —— 数据帧的重绘面积从整页降到 60%，
// 外加上传面积同比缩小，这是把"每秒一张全屏"打掉的主杠杆。
//
// 矩形四周各留 6px：卡片圆角和描边不出界，但留点余量防卡影擦不干净。
func (p *OverviewPage) DataRects(d *DrawCtx) []Rect {
	return []Rect{
		{PadX - 6, ovRingY - 6, CardW + 12, ovRingH + 12},
		{PadX - 6, ovNetY - 6, CardW + 12, ovNetH + 12},
		{PadX - 6, ovInfoY - 6, CardW + 12, ovInfoH + 12},
	}
}

// 首页纵向布局（四张卡 + 三处 14px 间距，底边统一落在 888）。
//
// 网络卡从 138 长到 190：洋哥要求它照参考卡片做「标题 + 右上双速率 + 曲线」，
// 多出来的高度从负载卡（200→192）和系统信息卡（240→196）里挤，
// 信息卡把行距从 46 收到 36 即可放下四行 —— 底部不再有空白。
const (
	ovWxY   = 104
	ovWxH   = 164
	ovRingY = 282
	ovRingH = 192
	ovNetY  = 488
	ovNetH  = 190
	ovInfoY = 692
	ovInfoH = 196
)

func (p *OverviewPage) Draw(c *Canvas, d *DrawCtx) {
	th, fs := d.Theme, d.Fonts
	m := p.mon

	// 🔴 天气图标已改**静态**（洋哥拍板）：不再 KeepAnim、不再上报动画速度。
	// 概览页由此从"常驻动画页"降级为"静态页" —— 数据代数没变就整帧跳过
	// （planFrame 的 frameSkip），这一刀砍掉了曾经占大头的图标动画帧。
	// 天气快照变化时由天气戳校验触发一次全屏重画，图标随之更新。

	// ---------------- Clip 守卫 ----------------
	//
	// 局部重绘的代价模型：clip 只裁掉**像素**，裁不掉**代码** —— 整页 Draw
	// 每帧都会全量执行，fmt.Sprintf、字形度量这些前置计算一样都不少。
	// 数据帧要重画三张卡，等于把整页代码跑 3 遍。每张卡先做一次 clip 相交
	// 测试（一次函数调用），不相交就整段跳过。
	// 正确性：跳过的前提是"clip 与卡片矩形不相交"，被裁掉的像素本来就不会画出来。

	// ---------------- 天气（彩色，取当天天气的代表色做卡面色）----------------
	if c.ClipVisible(PadX, ovWxY, CardW, ovWxH) {
		wx := d.WeatherNow()
		kind := wx.Kind
		if kind == "" {
			kind = "overcast"
		}
		wxAcc := weatherAccent(kind)

		DrawCardA(c, th, PadX, ovWxY, CardW, ovWxH, wxAcc)

	// ---- 版面：左图标 / 右一条统一的对齐轴 ----
	//
	// 上一版是"图标 + 一坨左对齐文字"：三行文字宽度越往下越长、右侧留一大片空白，
	// 行与行之间也没有分隔，看着松散。现在文字区收成一条**固定左边界**（tx），
	// 自上而下三层信息各占一行、行距均匀，最后用一条发丝线把"当前读数"
	// 与"今日区间"分开 —— 版面立刻有了秩序，右侧也不再空。
	const wxiR = 48
	// 图标中心略高于卡片中线：云体在上、雨雪落在下方，视觉重心比几何中心低，
	// 参数往上提 6px 之后整体才看着居中。
	//
	// 相位冻结在 2.856s：这是各天气的"代表姿态" ——
	//   雾：淡日圆盘呼吸到满相（0.55t = π/2）；
	//   雷：闪电包络正好在峰值（ph = mod(2.856, 2.8) = 0.056 → u ≈ 0.16）；
	//   雨/雪：粒子群散在半程，没有哪个粒子恰好隐没。
	// 每次天气源刷新（30 分钟）重画一次，姿态永远一致、可核对。
	DrawWeatherIcon(c, PadX+62, ovWxY+76, wxiR, kind, 2.856)

	tx := PadX + 124
	rEdge := PadX + CardW - 22

	if wx.OK {
		// 第一层：天气描述（语义色）+ 城市（次级灰）
		DrawPartsL(c, fs, tx, ovWxY+38, FzSmall, []TPart{
			{S: wx.Desc, Col: Lit(wxAcc, 0.30)},
			{S: "  ·  ", Col: th.Faint},
			{S: trim(wx.City, 6), Col: th.Dim},
		})

		// 第二层：当前温度。数字保持近白 —— 它是"气温"，不该被染成 CPU 橙或网络蓝；
		// 颜色已经由图标、卡面与描述文字承担了。
		drawWeatherTemp(c, fs, tx, ovWxY+52, fmt.Sprintf("%.0f", wx.TempC), "°C",
			th.Text, th.Dim)

		// 这里原本有一条发丝线，用来分隔"当前读数"与"今日区间"。
		// 但温度是 62px 的大字，行盒底部正好压到这条线上，观感变成
		// "给数字加了条下划线" —— 线条在数字下方 4px 处横穿，既没有
		// 起到分层作用，又让温度看着像被划掉。删除后靠行距本身分层。
		//
		// 第三层：左「今日区间」右「湿度」，两端对齐 —— 与顶栏同一套对齐语言
		DrawPartsL(c, fs, tx, ovWxY+144, FzSmall, []TPart{
			{S: fmt.Sprintf("%.0f", wx.MinC), Col: th.Sub, Latin: true},
			{S: "° ~ ", Col: th.Dim, Latin: true},
			{S: fmt.Sprintf("%.0f", wx.MaxC), Col: th.Sub, Latin: true},
			{S: "°", Col: th.Dim, Latin: true},
		})
		if wx.Humidity > 0 {
			DrawPartsR(c, fs, rEdge, ovWxY+144, FzSmall, []TPart{
				{S: "湿度 ", Col: th.Dim},
				{S: fmt.Sprintf("%d", wx.Humidity), Col: th.Sub, Latin: true},
				{S: "%", Col: th.Sub, Latin: true},
			})
		}
	} else {
		DrawText(c, fs, "天气获取中…", tx, ovWxY+74, FzBody, th.Dim)
	}
	} // 天气卡 clip 守卫结束

	// ---------------- CPU / 内存 负载（三态环：≤50 绿 / >50 黄 / ≥90 红）----------------
	if c.ClipVisible(PadX, ovRingY, CardW, ovRingH) {
	DrawCard(c, th, PadX, ovRingY, CardW, ovRingH)
	DrawCardTitleA(c, fs, th, PadX+22, ovRingY+22, "负载", P.Cpu)

	cpu := clamp(d.Ease("ov.cpu", m.CPUPercent), 0, 100)
	mem := clamp(d.Ease("ov.mem", m.MemPercent), 0, 100)
	ovRingCY := ovRingY + 96
	DrawMiniRing(c, fs, th, 116, ovRingCY, 56, 47, cpu/100, th.OnLoad(cpu),
		"CPU", fmt.Sprintf("%.0f%%", cpu), FzLarge)
	DrawMiniRing(c, fs, th, 260, ovRingCY, 56, 47, mem/100, th.OnLoad(mem),
		"内存", fmt.Sprintf("%.0f%%", mem), FzLarge)
	} // 负载卡 clip 守卫结束

	// ---------------- 网络（照洋哥给的参考卡片：标题 + 右上双速率 + 曲线）----------------
	//
	// 参考卡片的结构：左上「网络」，右上两行「↑ 上传（蓝）」「↓ 下载（绿）」，
	// 下面是同一坐标系里的两条曲线（一条带面积、一条只画线）。
	// 本页照此实现；数值口径不变（MB/s，与网络页同源同精度）。
	if c.ClipVisible(PadX, ovNetY, CardW, ovNetH) {
	DrawCardA(c, th, PadX, ovNetY, CardW, ovNetH, P.NetDown)
	titleTop := ovNetY + 20
	DrawCardTitleA(c, fs, th, PadX+22, titleTop, "网络", P.NetDown)
	iface := m.NetIface
	if iface == "" {
		iface = "—"
	}
	DrawText(c, fs, iface, PadX+22+InkWidth(fs, "网络", FzTiny)+18, titleTop+3, FzTiny, th.Dim)

	// 两行速率的基准线：第一行与标题的可见字形中心齐平，第二行下移 27px
	rowCY := titleTop + 12
	if tim := glyph(fs, FzTiny, "网络", th.Title); tim.inkH() > 0 {
		rowCY = titleTop + (tim.iy0+tim.iy1)/2
	}
	dn := d.Ease("ov.dn", m.NetRecvMBps)
	up := d.Ease("ov.up", m.NetSendMBps)
	DrawRateRow(c, fs, th, PadX+CardW-22, rowCY, up, P.NetUp, true)       // ↑ 上传（蓝）
	DrawRateRow(c, fs, th, PadX+CardW-22, rowCY+27, dn, P.NetDown, false) // ↓ 下载（绿）

	// 曲线：两条曲线共用同一量程（取两者峰值留 15% 余量），
	// 这样上下行的高度关系可以直接比较；量程不同会让细的那条被拉大。
	send := make([]float64, 0, len(m.NetHistory))
	recv := make([]float64, 0, len(m.NetHistory))
	for _, v := range m.NetHistory {
		send = append(send, v[0])
		recv = append(recv, v[1])
	}
	scale := 0.1
	for _, v := range m.NetHistory {
		if v[0] > scale {
			scale = v[0]
		}
		if v[1] > scale {
			scale = v[1]
		}
	}
	scale *= 1.15
	chartX, chartY := PadX+22, ovNetY+76
	chartW, chartH := CardW-44, 96
	c.Curve(chartX, chartY, chartW, chartH, recv, scale, 60, P.NetDown, 2, 0.26) // 下载：绿，带面积
	c.Curve(chartX, chartY, chartW, chartH, send, scale, 60, P.NetUp, 2, 0)      // 上传：蓝，只画线
	} // 网络卡 clip 守卫结束

	// ---------------- 系统信息（淡蓝）----------------
	if c.ClipVisible(PadX, ovInfoY, CardW, ovInfoH) {
	DrawCardA(c, th, PadX, ovInfoY, CardW, ovInfoH, P.Sys)
	DrawCardTitleA(c, fs, th, PadX+22, ovInfoY+22, "系统信息", P.Sys)
	y := ovInfoY + 56
	DrawKV(c, fs, th, PadX+22, y, CardW-44, "主机名", trim(d.Host, 16), th.Text)
	y += 36
	DrawKV(c, fs, th, PadX+22, y, CardW-44, "局域网地址", ipOrDash(m.LocalIP), Lit(P.Mem, 0.22))
	y += 36
	pub, pubCol := "获取中", th.Dim
	if d.Net != nil {
		if pi := d.Net.PublicIP(); pi.OK && pi.IP != "" {
			pub, pubCol = pi.IP, Lit(P.NetUp, 0.30)
		}
	}
	DrawKV(c, fs, th, PadX+22, y, CardW-44, "公网 IP", pub, pubCol)
	y += 36
	DrawKV(c, fs, th, PadX+22, y, CardW-44, "已运行", formatUptime(m.UptimeSec), th.Sub)
	} // 系统信息卡 clip 守卫结束
}

// drawWeatherTemp 天气卡的当前温度：拉丁细体超大数字 + 贴顶的小单位。
//
// 单位没用 DrawNumUnitL 那种"右下角悬挂"（那是仪表读数的语法：单位与数字底边齐平）。
// 气温是一个读数、°C 是它的注脚，注脚放在右上角更安静，也正好与下面
// 「24° ~ 28°」里的 ° 形成同一套上标语言。
//
// 🔴 这里**不能**按"可见字形"去减 ix0 对齐。页面上的文字（DrawText / DrawPartsL）
// 都是按**行盒原点**落笔的，两种基准混用会让温度比上方「多云 · 苏州」右移几像素，
// 一条对齐轴就断了。统一走行盒原点。
func drawWeatherTemp(c *Canvas, fs *FontSet, x, yTop int, num, unit string, col, unitCol RGBA) {
	nim := glyphLatin(fs, FzBig, num, col)
	if nim.inkW() == 0 {
		return
	}
	blitGlyph(c, nim, x, yTop)
	if unit == "" {
		return
	}
	uim := glyphLatin(fs, FzSmall, unit, unitCol)
	if uim.inkW() == 0 {
		return
	}
	blitGlyph(c, uim, x+nim.w+4, yTop+4)
}

// coreCount 取 CPU 逻辑核心数（优先用 cpu.Counts，退化为已采样的核心数）
func coreCount(m *monitor.SystemMonitor) int {
	if m.CPUCoreNum > 0 {
		return m.CPUCoreNum
	}
	return len(m.CPUCores)
}

// ============================================================
//  2. CPU 与内存（合并一页）
// ============================================================

type PerfPage struct{ mon *monitor.SystemMonitor }

func NewPerfPage(mon *monitor.SystemMonitor) *PerfPage { return &PerfPage{mon: mon} }

func (p *PerfPage) PageName() string { return "CPU / 内存" }

// 第二页纵向布局。
//
// 双环卡原本底部还有一行「系统负载 1.24 / 1.10 / 0.98」—— 洋哥要求删除，
// 因为本页已经有 CPU/内存 双环 + 核心负载 + 历史曲线，再叠一个 loadavg
// 只会重复表达"机器忙不忙"，且与顶栏温度挤在一起。删掉后卡片收窄 20px。
//
// 2026-09-27 内存明细改版：2×2 网格里放不下「交换 0.4 / 4.0 GB」这个 11 字符的值
// （单列仅 138px），标签与数字挤成一坨。于是把「交换」提为独占一行的
// 「标签 + 占比条 + 值」，同时补上原先缺失的「总容量」凑满 2×2。
// 内存卡因此加高 32px，从核心负载卡（-24）与历史卡（-8）里匀出来，
// 底部仍统一落在 888。
const (
	pfRingY = 104
	pfRingH = 238
	pfMemY  = 356
	pfMemH  = 184
	pfCoreY = 554
	pfCoreH = 172
	pfHistY = 740
	pfHistH = 148
)

func (p *PerfPage) Draw(c *Canvas, d *DrawCtx) {
	th, fs := d.Theme, d.Fonts
	m := p.mon

	// ---------------- 双环：CPU（橙）+ 内存（青）----------------
	DrawCard(c, th, PadX, pfRingY, CardW, pfRingH)
	cpu := clamp(d.Ease("pf.cpu", m.CPUPercent), 0, 100)
	mem := clamp(d.Ease("pf.mem", m.MemPercent), 0, 100)
	ringCY := pfRingY + 96
	DrawMiniRing(c, fs, th, 116, ringCY, 56, 47, cpu/100, th.OnLoad(cpu),
		"CPU", fmt.Sprintf("%.0f%%", cpu), FzLarge)
	DrawMiniRing(c, fs, th, 260, ringCY, 56, 47, mem/100, th.OnLoad(mem),
		"内存", fmt.Sprintf("%.0f%%", mem), FzLarge)

	DrawHairline(c, th, PadX+40, pfRingY+198, CardW-80)
	DrawPartsC(c, fs, c.W/2, pfRingY+222, FzTiny, []TPart{
		{S: "温度 ", Col: th.Dim},
		{S: fmt.Sprintf("%.1f", m.CPUTemp), Col: th.OnTemp(P.Cpu, m.CPUTemp), Latin: true},
		{S: "°C", Col: th.Dim, Latin: true},
		{S: "  ·  ", Col: th.Faint},
		{S: fmt.Sprintf("%d", coreCount(m)), Col: th.Text, Latin: true},
		{S: " 核心", Col: th.Dim},
		{S: "  ·  ", Col: th.Faint},
		{S: fmt.Sprintf("%d", m.ProcCount), Col: th.Text, Latin: true},
		{S: " 进程", Col: th.Dim},
	})

	// ---------------- 内存明细（2×2 分项 + 交换独占一行）----------------
	//
	// 改版原因：原来 2×2 网格里的「交换 0.4 / 4.0 GB」有 11 个字符，而单列只有
	// 138px —— 值把列内空隙吃光，标签和数字挤成一坨。现在把四项"分项容量"
	// 排成 2×2（顺带补上原本缺失的「总容量」，信息更完整），把「交换」单独
	// 拿出来做一整行：标签 + 占比条 + 右对齐的「已用 / 总量」。
	// 宽度够了，还让"交换用了多少"变成一眼可读的条。
	DrawCardA(c, th, PadX, pfMemY, CardW, pfMemH, P.Mem)
	DrawCardTitleA(c, fs, th, PadX+22, pfMemY+22, "内存明细", P.Mem)

	memCol := Lit(P.Mem, 0.28)
	memColW := (CardW - 44) / 2
	DrawKV(c, fs, th, PadX+22, pfMemY+58, memColW-14,
		"已用", fmt.Sprintf("%.1f GB", m.MemUsedGB), memCol)
	DrawKV(c, fs, th, PadX+22+memColW, pfMemY+58, memColW-14,
		"可用", fmt.Sprintf("%.1f GB", m.MemAvailGB), memCol)
	DrawKV(c, fs, th, PadX+22, pfMemY+100, memColW-14,
		"缓存", fmt.Sprintf("%.1f GB", m.MemCacheGB), th.Sub)
	DrawKV(c, fs, th, PadX+22+memColW, pfMemY+100, memColW-14,
		"总容量", fmt.Sprintf("%.1f GB", m.MemTotalGB), th.Sub)

	DrawHairline(c, th, PadX+22, pfMemY+130, CardW-44)

	// 交换：整行 —— 标签 / 占比条 / 值
	sw := clamp(d.Ease("pf.swap", m.SwapPercent), 0, 100)
	rowCY := pfMemY + 146
	DrawText(c, fs, "交换", PadX+22, rowCY+2, FzTiny, th.Dim)

	valStr := "未启用"
	if m.SwapTotalGB > 0 {
		valStr = fmt.Sprintf("%.1f / %.1f GB", m.SwapUsedGB, m.SwapTotalGB)
	}
	rx := PadX + CardW - 22
	vw := mixedWidth(fs, FzSmall, valStr)
	barX := PadX + 22 + 46
	barW := rx - vw - 18 - barX
	if barW < 40 {
		barW = 40
	}
	// 底轨带内存色相；填充走负载三态色（交换用量超过 50% 跳琥珀黄，≥90% 砖红）
	c.FillRoundRect(barX, rowCY+7, barW, 6, 3, WithA(P.Mem, 46))
	if fw := int(float64(barW)*sw/100 + 0.5); fw > 0 {
		if fw < 6 {
			fw = 6
		}
		c.FillRoundRect(barX, rowCY+7, fw, 6, 3, th.OnLoad(sw))
	}
	DrawTextMixR(c, fs, rx, rowCY, FzSmall, th.Sub, valStr)

	// ---------------- 核心负载（两列，橙色）----------------
	DrawCardA(c, th, PadX, pfCoreY, CardW, pfCoreH, P.Cpu)
	DrawCardTitleA(c, fs, th, PadX+22, pfCoreY+22, "核心负载", P.Cpu)
	const colW = 138
	x1, x2 := PadX+22, PadX+22+colW+16
	n := coreCount(m)
	if n <= 0 {
		n = 4
	}
	if n > 8 {
		n = 8
	}
	rows := (n + 1) / 2
	rowH := 56
	if avail := 120 / rows; avail < rowH {
		rowH = avail
	}
	y0 := pfCoreY + 66
	for i := 0; i < n; i++ {
		col := x1
		if i%2 == 1 {
			col = x2
		}
		yy := y0 + (i/2)*rowH
		var v float64
		if i < len(m.CPUCores) {
			v = clamp(d.Ease("pf.c"+strconvItoa(i), m.CPUCores[i]), 0, 100)
		}
		DrawText(c, fs, strconvItoa(i+1), col, yy+2, FzTiny, th.Dim)
		DrawBar(c, th, col+18, yy+9, 68, 4, v/100, th.OnLoad(v))
		DrawTextLR(c, fs, fmt.Sprintf("%.0f%%", v), col+colW, yy-2, FzSmall, Lit(P.Cpu, 0.28))
	}

	// ---------------- CPU 历史（橙色曲线）----------------
	DrawCardA(c, th, PadX, pfHistY, CardW, pfHistH, P.Cpu)
	DrawCardTitleA(c, fs, th, PadX+22, pfHistY+22, "CPU 历史 · 60 秒", P.Cpu)
	c.AreaChart(PadX+22, pfHistY+52, CardW-44, pfHistH-72,
		m.CPUHistory, autoTop(m.CPUHistory, 10), 60, P.Cpu, P.Cpu, 1)
}

// ============================================================
//  3. 硬盘页
// ============================================================

type DiskPage struct{ mon *monitor.SystemMonitor }

func NewDiskPage(mon *monitor.SystemMonitor) *DiskPage { return &DiskPage{mon: mon} }

func (p *DiskPage) PageName() string { return "硬盘" }

const (
	dkCardH = 232
	dkStep  = 252
)

func (p *DiskPage) Draw(c *Canvas, d *DrawCtx) {
	th, fs := d.Theme, d.Fonts
	vols := p.mon.Disk.GetVolumes()

	DrawText(c, fs, fmt.Sprintf("共 %d 个存储卷", len(vols)), PadX+2, 108, FzTiny, th.Dim)

	if len(vols) == 0 {
		DrawCard(c, th, PadX, 140, CardW, 160)
		DrawTextC(c, fs, "无存储数据", 188, 212, FzBody, th.Dim)
		return
	}

	y := 132
	for i, v := range vols {
		if i >= 3 {
			break
		}
		// 容量环/条走负载三态色（≤50 绿 / >50 黄 / ≥90 红）—— 2026-09-27 洋哥指定；
		// 卡片描边、卷名、容量数字仍保留介质语义色（SSD 紫 / HDD 蓝 / eMMC 青）
		dcol := P.DiskColor(v.DeviceType)
		pct := clamp(d.Ease("disk."+strconvItoa(i), v.UsedPercent), 0, 100)
		lv := th.OnLoad(pct)

		DrawCardA(c, th, PadX, y, CardW, dkCardH, dcol)

		DrawMiniRing(c, fs, th, PadX+74, y+112, 44, 38, pct/100, lv,
			"", fmt.Sprintf("%.0f%%", pct), FzMid)

		nx := PadX + 140
		// 卷名（真实卷标优先）+ 底层设备名
		drawVolumeTitle(c, fs, th, nx, y+44, trim(v.VolName, 9), v.BaseDevice, dcol)

		DrawBar(c, th, nx, y+100, 172, 4, pct/100, lv)
		capParts := []TPart{
			{S: fmt.Sprintf("%.0f", v.UsedGB), Col: Lit(dcol, 0.30), Latin: true},
			{S: " GB", Col: th.Dim},
		}
		if v.TotalGB > 0 {
			capParts = []TPart{
				{S: fmt.Sprintf("%.0f", v.UsedGB), Col: Lit(dcol, 0.30), Latin: true},
				{S: " / ", Col: th.Dim, Latin: true},
				{S: fmt.Sprintf("%.0f", v.TotalGB), Col: th.Sub, Latin: true},
				{S: " GB", Col: th.Dim},
			}
		}
		DrawPartsL(c, fs, nx, y+138, FzTiny, capParts)

		// 底部：温度 + 介质类型（都是实测值）。
		// 硬盘温度的合理阈值是 45 / 58 °C（不是 CPU 的 72 / 82）。
		tcol := th.OnAt(dcol, v.Temp, 45, 58)
		types := Lit(dcol, 0.18)
		parts := []TPart{{S: v.DeviceType, Col: types}}
		if v.Temp > 0 {
			parts = []TPart{
				{S: fmt.Sprintf("%.0f", v.Temp), Col: tcol, Latin: true},
				{S: "°C", Col: tcol, Latin: true},
				{S: "  ·  ", Col: th.Faint},
				{S: v.DeviceType, Col: types},
			}
		}
		DrawPartsL(c, fs, nx, y+176, FzTiny, parts)

		y += dkStep
	}
}

// drawVolumeTitle 卷名（大，前面带一根语义色条）+ 底层设备名（小，紧跟其后）
func drawVolumeTitle(c *Canvas, fs *FontSet, th *Theme, x, y int, name, device string, accent RGBA) {
	c.FillRoundRect(x, y+3, 3, 14, 1, WithA(accent, 220))
	w := DrawText(c, fs, name, x+11, y, FzSmall, th.Text)
	if device == "" {
		return
	}
	DrawText(c, fs, device, x+11+w+10, y+3, FzTiny, th.Dim)
}

// ============================================================
//  4. 网络页
// ============================================================

type NetworkPage struct{ mon *monitor.SystemMonitor }

func NewNetworkPage(mon *monitor.SystemMonitor) *NetworkPage { return &NetworkPage{mon: mon} }

func (p *NetworkPage) PageName() string { return "网络" }

const (
	nwRateY = 104
	nwRateH = 244
	nwCharY = 362
	nwCharH = 366
	nwSumY  = 742
	nwSumH  = 140
)

func (p *NetworkPage) Draw(c *Canvas, d *DrawCtx) {
	th, fs := d.Theme, d.Fonts
	m := p.mon

	up := d.Ease("net.up", m.NetSendMBps)
	dn := d.Ease("net.dn", m.NetRecvMBps)

	iface := m.NetIface
	if iface == "" {
		iface = "—"
	}

	// ---------------- 实时速率（下载绿 / 上传蓝）----------------
	DrawCardA(c, th, PadX, nwRateY, CardW, nwRateH, P.NetDown)
	DrawCardTitleA(c, fs, th, PadX+22, nwRateY+22, "实时速率", P.NetDown)
	DrawTextR(c, fs, iface, PadX+CardW-22, nwRateY+18, FzTiny, th.Dim)
	DrawText(c, fs, "下载", PadX+22, nwRateY+64, FzTiny, Lit(P.NetDown, 0.12))
	DrawNumUnitL(c, fs, PadX+22, nwRateY+122, fmt.Sprintf("%.2f", dn), "MB/s",
		FzLarge, FzTiny, Lit(P.NetDown, 0.30), th.Dim)
	DrawHairline(c, th, PadX+22, nwRateY+152, CardW-44)
	DrawText(c, fs, "上传", PadX+22, nwRateY+184, FzTiny, Lit(P.NetUp, 0.12))
	DrawNumUnitL(c, fs, PadX+22, nwRateY+242, fmt.Sprintf("%.2f", up), "MB/s",
		FzLarge, FzTiny, Lit(P.NetUp, 0.30), th.Dim)

	// ---------------- 曲线 ----------------
	send := make([]float64, 0, len(m.NetHistory))
	recv := make([]float64, 0, len(m.NetHistory))
	for _, v := range m.NetHistory {
		send = append(send, v[0])
		recv = append(recv, v[1])
	}
	pkSend, pkRecv := 0.0, 0.0
	for _, v := range m.NetHistory {
		if v[0] > pkSend {
			pkSend = v[0]
		}
		if v[1] > pkRecv {
			pkRecv = v[1]
		}
	}
	scale := pkSend
	if pkRecv > scale {
		scale = pkRecv
	}
	scale *= 1.15
	if scale < 0.1 {
		scale = 0.1
	}

	DrawCard(c, th, PadX, nwCharY, CardW, nwCharH)
	DrawCardTitleA(c, fs, th, PadX+22, nwCharY+22, "流量曲线 · 60 秒", P.NetDown)
	DrawPartsL(c, fs, PadX+22, nwCharY+72, FzTiny, []TPart{
		{S: "下载", Col: Lit(P.NetDown, 0.15)},
		{S: "   峰值 ", Col: th.Dim},
		{S: fmt.Sprintf("%.2f", pkRecv), Col: Lit(P.NetDown, 0.25), Latin: true},
		{S: " MB/s", Col: th.Dim, Latin: true},
	})
	c.AreaChart(PadX+22, nwCharY+72, CardW-44, 112, recv, scale, 60, P.NetDown, P.NetDown, 1)
	DrawPartsL(c, fs, PadX+22, nwCharY+228, FzTiny, []TPart{
		{S: "上传", Col: Lit(P.NetUp, 0.15)},
		{S: "   峰值 ", Col: th.Dim},
		{S: fmt.Sprintf("%.2f", pkSend), Col: Lit(P.NetUp, 0.25), Latin: true},
		{S: " MB/s", Col: th.Dim, Latin: true},
	})
	c.AreaChart(PadX+22, nwCharY+228, CardW-44, 112, send, scale, 60, P.NetUp, P.NetUp, 1)

	// ---------------- 累计流量（当日，绿 / 蓝）----------------
	DrawCardA(c, th, PadX, nwSumY, CardW, nwSumH, P.NetDown)
	DrawCardTitleA(c, fs, th, PadX+22, nwSumY+22, "累计流量 · 今日", P.NetDown)
	DrawKV(c, fs, th, PadX+22, nwSumY+66, CardW-44, "接收", fmtBytes(m.NetRxToday), Lit(P.NetDown, 0.30))
	DrawKV(c, fs, th, PadX+22, nwSumY+108, CardW-44, "发送", fmtBytes(m.NetTxToday), Lit(P.NetUp, 0.30))
}

// ============================================================
//  5. 系统页（风扇 / 关屏 / 主题）
//
//  关屏按钮**只放在本页**（洋哥要求）；不按则常亮。
// ============================================================

type SystemPage struct {
	mon      *monitor.SystemMonitor
	fan      *hardware.FanController
	bright   *hardware.BrightnessController
	webToken string // Web 面板令牌：屏幕是它的唯一展示渠道（物理接触=授权）
}

func NewSystemPage(mon *monitor.SystemMonitor, fan *hardware.FanController, br *hardware.BrightnessController, webToken string) *SystemPage {
	return &SystemPage{mon: mon, fan: fan, bright: br, webToken: webToken}
}

func (p *SystemPage) PageName() string { return "系统" }

func fanModeCN(m string) string {
	switch m {
	case "low":
		return "低速"
	case "medium":
		return "中速"
	case "high":
		return "高速"
	default:
		return "自动"
	}
}

var fanModes = []string{"auto", "low", "medium", "high"}

func fanModeIndex(m string) int {
	for i, v := range fanModes {
		if v == m {
			return i
		}
	}
	return 0
}

// 风扇分段控件的几何
const (
	fanTabX = PadX + 22
	fanTabW = CardW - 44
)

// 系统页卡片纵向布局。
//
// 洋哥要求「关闭屏幕」放在**最下面**：它是个破坏性操作（一按屏幕就黑），
// 放到最后一格既是视觉终点，也离手指最远，最不容易误触。
// 顺序：CPU 风扇 → 机箱风扇 → 外观主题 → 屏幕（关屏）。
const (
	sysCardH     = 156
	sysCardCPUY  = 104
	sysCardHDDY  = 274
	sysCardThemY = 444
	sysCardThemH = 268
	sysCardOffY  = 726
	sysCardOffH  = 162
	sysBtnY      = sysCardOffY + 54
	sysBtnH      = 52
)

func (p *SystemPage) Draw(c *Canvas, d *DrawCtx) {
	th, fs := d.Theme, d.Fonts

	cpuMode, hddMode := "auto", "auto"
	cpuRPM, hddRPM := 0, 0
	if p.fan != nil {
		cpuMode = p.fan.GetCPUMode()
		hddMode = p.fan.GetHDDMode()
		cpuRPM, hddRPM = p.fan.GetFanSpeeds()
	}
	labels := []string{fanModeCN("auto"), fanModeCN("low"), fanModeCN("medium"), fanModeCN("high")}

	// 两条调速曲线的**被控量**（实测值，与各自卡片上写的一致）：
	//   CPU 风扇  ← CPU 封装温度（coretemp Package id 0）
	//   机箱风扇  ← 所有硬盘里最高的那个温度（真实盘温，不是主板传感器）
	cpuTemp, diskTemp := 0.0, 0.0
	if d.Mon != nil {
		cpuTemp = d.Mon.CPUTemp
		diskTemp = d.Mon.Disk.MaxDiskTemp()
	}

	// ---- CPU 风扇（橙）----
	drawFanCard(c, fs, th, sysCardCPUY, "CPU 风扇", cpuMode, cpuRPM, labels, P.Cpu,
		cpuTemp, th.OnTemp(P.Cpu, cpuTemp))
	// ---- 机箱风扇（青）----
	drawFanCard(c, fs, th, sysCardHDDY, "机箱风扇", hddMode, hddRPM, labels, P.Mem,
		diskTemp, th.OnAt(P.Mem, diskTemp, 45, 58))

	// ---- 外观主题（上滑切换，把隐藏手势写出来）----
	DrawCard(c, th, PadX, sysCardThemY, CardW, sysCardThemH)
	DrawCardTitle(c, fs, th, PadX+22, sysCardThemY+22, "外观主题")
	// 主题名用拉丁细体（Cantarell-Thin），与全屏排版语言一致
	DrawTextLC(c, fs, d.Theme.Name, 188, sysCardThemY+58, FzLarge, th.Text)
	DrawTextMixC(c, fs, 188, sysCardThemY+124, FzTiny, th.Dim, "上滑切换主题")
	DrawThemeChips(c, fs, th, PadX+22, sysCardThemY+164, CardW-44, 44,
		ThemeNames(), d.Theme.Name)
	// 底部放真实亮度读数（可被验证），而不是一句空泛的说明
	foot := "共 4 套低饱和配色"
	if p.bright != nil {
		if v, err := p.bright.Get(); err == nil {
			foot = fmt.Sprintf("当前亮度 %d", v)
		}
	}
	DrawTextMixC(c, fs, 188, sysCardThemY+230, FzTiny, th.Faint, foot)

	// ---- 屏幕（蓝）---- 放在最后一格
	DrawCardA(c, th, PadX, sysCardOffY, CardW, sysCardOffH, P.Sys)
	DrawCardTitleA(c, fs, th, PadX+22, sysCardOffY+22, "屏幕", P.Sys)
	DrawButton(c, fs, th, 88, sysBtnY, 200, sysBtnH, "关闭屏幕", P.Sys)
	// 底部脚注放两件事实：面板令牌（首次用网页控制面板时照抄）与唤醒提示
	offFoot := "不按则常亮 · 轻触任意处唤醒"
	if p.webToken != "" {
		offFoot = "面板令牌 " + p.webToken + " · 轻触任意处唤醒"
	}
	DrawTextMixC(c, fs, 188, sysCardOffY+126, FzTiny, th.Dim, offFoot)
}

// drawFanCard 风扇卡：标题 + 模式名 + 真实转速 + 驱动温度 + 分段控件。
//
// srcTemp / srcCol 是驱动这条曲线的**实测温度**及其状态色。把被控量写在卡片上，
// 「为什么是这个转速」就永远可核对 —— 洋哥的要求是「按真实硬盘温度调速」，
// 那屏幕上也应该看得到那个温度。
func drawFanCard(c *Canvas, fs *FontSet, th *Theme, y int, title, mode string, rpm int,
	labels []string, accent RGBA, srcTemp float64, srcCol RGBA) {
	DrawCardA(c, th, PadX, y, CardW, sysCardH, accent)
	DrawCardTitleA(c, fs, th, PadX+22, y+22, title, accent)
	DrawTextR(c, fs, fanModeCN(mode), PadX+CardW-22, y+18, FzSmall, th.Text)

	// 转速才是可被验证的事实 —— 用本风扇的语义色
	if rpm > 0 {
		DrawNumUnitL(c, fs, PadX+22, y+74, fmt.Sprintf("%d", rpm), "RPM",
			FzMid, FzTiny, Lit(accent, 0.28), th.Dim)
	} else {
		DrawNumUnitL(c, fs, PadX+22, y+74, "--", "RPM",
			FzMid, FzTiny, th.Dim, th.Dim)
	}

	// 驱动温度：与转速同一基线、右对齐
	if srcTemp > 0 {
		DrawPartsR(c, fs, PadX+CardW-22, y+74, FzTiny, []TPart{
			{S: "驱动温度 ", Col: th.Dim},
			{S: fmt.Sprintf("%.0f", srcTemp), Col: srcCol, Latin: true},
			{S: "°C", Col: srcCol, Latin: true},
		})
	}

	DrawTabs(c, fs, th, fanTabX, y+108, fanTabW, labels, fanModeIndex(mode), accent)
}

func (p *SystemPage) Tap(x, y int, d *DrawCtx) {
	if p.fan == nil {
		return
	}
	step := fanTabW / len(fanModes)
	pick := func() int {
		i := (x - fanTabX) / step
		if i < 0 {
			i = 0
		}
		if i >= len(fanModes) {
			i = len(fanModes) - 1
		}
		return i
	}
	switch {
	case y >= sysCardCPUY && y <= sysCardCPUY+sysCardH: // CPU 风扇卡
		p.fan.SetCPUMode(fanModes[pick()])
	case y >= sysCardHDDY && y <= sysCardHDDY+sysCardH: // 机箱风扇卡
		p.fan.SetHDDMode(fanModes[pick()])
	case y >= sysBtnY && y <= sysBtnY+sysBtnH: // 关屏按钮
		if d.OnScreenOff != nil {
			d.OnScreenOff()
		} else if p.bright != nil {
			p.bright.Set(0)
		}
	}
}

// ============================================================
//  工具
// ============================================================

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ipOrDash 空 IP 显示为破折号，避免出现编造的占位地址
func ipOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// trim 按字符（含中文）截断
func trim(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}
