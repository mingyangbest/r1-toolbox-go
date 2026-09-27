package ui

import (
	"math"
	"strconv"
	"sync"
)

// ============================================================
//  颜色
// ============================================================

type RGBA struct{ R, G, B, A uint8 }

func C(r, g, b uint8) RGBA { return RGBA{r, g, b, 255} }

// HexColor 解析 "#RRGGBB"，失败返回白色
func HexColor(s string) RGBA {
	if len(s) != 7 || s[0] != '#' {
		return RGBA{255, 255, 255, 255}
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return RGBA{255, 255, 255, 255}
	}
	return RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
}

// WithA 返回带指定透明度的颜色副本
func WithA(c RGBA, a uint8) RGBA { c.A = a; return c }

// Lit 把颜色朝白色提亮 t（0..1）。
// 语义色直接当正文色会略显沉闷，提亮一点既保住色相、又保证可读性 ——
// 用于环中心的大数值、KV 行的彩色值。
func Lit(c RGBA, t float64) RGBA {
	if t <= 0 {
		return c
	}
	if t >= 1 {
		return RGBA{255, 255, 255, c.A}
	}
	f := func(x uint8) uint8 { return uint8(float64(x) + (255-float64(x))*t + 0.5) }
	return RGBA{f(c.R), f(c.G), f(c.B), c.A}
}

// Lerp 颜色线性插值 t∈[0,1]
func Lerp(a, b RGBA, t float64) RGBA {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	f := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5)
	}
	return RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), f(a.A, b.A)}
}

// ============================================================
//  Canvas —— 像素级绘图缓冲（RGBA，内存顺序 R,G,B,A）
// ============================================================

type Canvas struct {
	W, H int
	Pix  []byte // len = W*H*4

	base []byte // 静态底图缓存
	hasB bool

	// 裁剪区（逻辑坐标，未含偏移）
	clipX0, clipY0, clipX1, clipY1 int
	// 全局偏移（用于页面滑动）
	ox, oy int
	// 全局透明度（用于页面淡入淡出）
	ga float64
}

func NewCanvas(w, h int) *Canvas {
	c := &Canvas{W: w, H: h, Pix: make([]byte, w*h*4), ga: 1}
	c.clipX1, c.clipY1 = w, h
	return c
}

// SetGlobalAlpha 设置后续绘制的全局透明度（0..1）
func (c *Canvas) SetGlobalAlpha(a float64) {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	c.ga = a
}

// SetBase 把当前画面固化为静态底图
func (c *Canvas) SetBase() {
	if c.base == nil {
		c.base = make([]byte, len(c.Pix))
	}
	copy(c.base, c.Pix)
	c.hasB = true
}

// HasBase 是否已有底图
func (c *Canvas) HasBase() bool { return c.hasB }

// Reset 恢复为底图
func (c *Canvas) Reset() {
	if c.hasB {
		copy(c.Pix, c.base)
	} else {
		for i := range c.Pix {
			c.Pix[i] = 0
		}
	}
}

// RestoreRegion 把矩形区域恢复成静态底图。
//
// 页面滑动时用：两页各自在自己那半屏里重画，但页面本身只画卡片、
// 纸面是共用的底图 —— 如果不清底，对方的卡片就会从边界处透出来。
// 逐行 copy，比逐像素 Blend 快一个量级。
func (c *Canvas) RestoreRegion(x, y, w, h int) {
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
		return
	}
	for yy := y; yy < y+h; yy++ {
		off := (yy*c.W + x) << 2
		end := off + w*4
		if c.hasB {
			copy(c.Pix[off:end], c.base[off:end])
		} else {
			for i := off; i < end; i++ {
				c.Pix[i] = 0
			}
		}
	}
}

func (c *Canvas) Clear(col RGBA) {
	r, g, b := col.R, col.G, col.B
	for i := 0; i < len(c.Pix); i += 4 {
		c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = r, g, b, 255
		_ = i
	}
}

// SetClip 设置裁剪矩形（逻辑坐标）
func (c *Canvas) SetClip(x, y, w, h int) {
	c.clipX0, c.clipY0 = x, y
	c.clipX1, c.clipY1 = x+w, y+h
}

func (c *Canvas) ClearClip() {
	c.clipX0, c.clipY0, c.clipX1, c.clipY1 = 0, 0, c.W, c.H
}

// SetOffset 设置绘制全局偏移（页面滑动用）
func (c *Canvas) SetOffset(dx, dy int) { c.ox, c.oy = dx, dy }

// ------------------------------------------------------------
//  裁剪粗筛
// ------------------------------------------------------------
//
// 页面滑动时每帧要把两页各画一遍，而每页只有一半左右真的落在屏幕上。
// 下面的 helper 让图元在**遍历像素之前**就把循环范围收进裁剪区 ——
// 否则被裁掉的那一半仍要逐像素算覆盖率、再在 Blend 里被拒绝，
// 白白吃掉一半帧时间（实测滑动从 60fps 目标掉到 20fps，主因就在这里）。
//
// 注意：轨迹范围收窄**不改变任何像素结果**。抗锯齿覆盖率是按像素中心与
// 形状的距离算的，与遍历起点无关；被裁掉的像素本来也画不上。

// clipMinX 裁剪区在逻辑坐标（未含偏移）下的左边界
func (c *Canvas) clipMinX() int { return c.clipX0 - c.ox }

// clipMaxX 裁剪区在逻辑坐标下的右边界（开区间，最大有效 x = 返回值-1）
func (c *Canvas) clipMaxX() int { return c.clipX1 - c.ox }

func (c *Canvas) clipMinY() int { return c.clipY0 - c.oy }
func (c *Canvas) clipMaxY() int { return c.clipY1 - c.oy }

// clipVisible 逻辑矩形是否与裁剪区相交
func (c *Canvas) clipVisible(x, y, w, h int) bool {
	return x < c.clipMaxX() && x+w > c.clipMinX() &&
		y < c.clipMaxY() && y+h > c.clipMinY()
}

// ClipVisible 页面用：当前 clip 是否与卡片矩形相交。
// 不相交时页面可以整段跳过绘制代码 —— clip 只能裁掉像素，
// 裁不掉 fmt.Sprintf、字形度量、图标逐像素数学这些前置计算。
func (c *Canvas) ClipVisible(x, y, w, h int) bool {
	return c.clipVisible(x, y, w, h)
}

// clampX 把遍历范围 [x0,x1] 收进裁剪区
func (c *Canvas) clampX(x0, x1 int) (int, int) {
	if x0 < c.clipMinX() {
		x0 = c.clipMinX()
	}
	if x1 > c.clipMaxX()-1 {
		x1 = c.clipMaxX() - 1
	}
	return x0, x1
}

// clampY 同上，纵向
func (c *Canvas) clampY(y0, y1 int) (int, int) {
	if y0 < c.clipMinY() {
		y0 = c.clipMinY()
	}
	if y1 > c.clipMaxY()-1 {
		y1 = c.clipMaxY() - 1
	}
	return y0, y1
}

// ------------------------------------------------------------
//  基础像素混合
// ------------------------------------------------------------

// Blend 在 (x,y) 用 alpha 混合一个颜色。坐标会叠加全局偏移。
func (c *Canvas) Blend(x, y int, col RGBA, a float64) {
	if a <= 0 {
		return
	}
	x += c.ox
	y += c.oy
	if x < c.clipX0 || x >= c.clipX1 || y < c.clipY0 || y >= c.clipY1 {
		return
	}
	a *= c.ga
	if a > 1 {
		a = 1
	}
	if a <= 0 {
		return
	}
	if col.A != 255 {
		a *= float64(col.A) / 255.0
		if a <= 0 {
			return
		}
	}
	i := (y*c.W + x) << 2
	inv := 1 - a
	c.Pix[i] = uint8(float64(col.R)*a + float64(c.Pix[i])*inv)
	c.Pix[i+1] = uint8(float64(col.G)*a + float64(c.Pix[i+1])*inv)
	c.Pix[i+2] = uint8(float64(col.B)*a + float64(c.Pix[i+2])*inv)
	c.Pix[i+3] = 255
}

// ------------------------------------------------------------
//  矩形
// ------------------------------------------------------------

func (c *Canvas) FillRect(x, y, w, h int, col RGBA) {
	if w <= 0 || h <= 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	x0, x1 := c.clampX(x, x+w-1)
	y0, y1 := c.clampY(y, y+h-1)
	for yy := y0; yy <= y1; yy++ {
		for xx := x0; xx <= x1; xx++ {
			c.Blend(xx, yy, col, 1)
		}
	}
}

// FillRectGrad 垂直渐变矩形
func (c *Canvas) FillRectGrad(x, y, w, h int, top, bot RGBA) {
	if w <= 0 || h <= 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	x0, x1 := c.clampX(x, x+w-1)
	for yy := 0; yy < h; yy++ {
		py := y + yy
		if py < c.clipMinY() || py > c.clipMaxY()-1 {
			continue
		}
		t := 0.0
		if h > 1 {
			t = float64(yy) / float64(h-1)
		}
		col := Lerp(top, bot, t)
		for xx := x0; xx <= x1; xx++ {
			c.Blend(xx, py, col, 1)
		}
	}
}

// sdRoundRect 圆角矩形有符号距离（负=内部）
func sdRoundRect(px, py, x, y, w, h, r float64) float64 {
	cx := math.Abs(px-(x+w/2)) - (w/2 - r)
	cy := math.Abs(py-(y+h/2)) - (h/2 - r)
	return math.Hypot(math.Max(cx, 0), math.Max(cy, 0)) + math.Min(math.Max(cx, cy), 0) - r
}

// ------------------------------------------------------------
//  圆角遮罩缓存
// ------------------------------------------------------------
//
// 🔴 这是这一版最大的性能改动，起因是实测出来的一个数字：
// 首页的"页面绘制"占 31ms/帧，而"纹理上传"只有 1.3ms ——
// 瓶颈不在上传（一直在怀疑它），89% 的 CPU 全烧在绘制里。
//
// 再往里拆：卡片是屏幕上面积最大的元素。首页四张卡加上描边，
// 每帧要遍历约 57 万个像素，而旧实现**每个像素**都要算一次
// sdRoundRect（两次 Abs、两次 Max/Min、一次 Hypot，约 55ns）：
//   57 万 × 55ns ≈ 31ms —— 整个帧时间就是它，没有别的。
//
// 但圆角的形状只由 (w,h,r) 决定，与位置、颜色无关；屏幕上的卡片
// 尺寸是固定的几种，也就是说每帧二十次重绘都在算同一张遮罩。
// 所以把遮罩按尺寸缓存：构建一次，之后每帧只有"查表 + 混合"。
//
// 同时缓存每行的**满覆盖区间**（fullL/fullR）：矩形的绝大多数像素
// 覆盖率是 255（整行除了圆角两端都是实的），这些像素可以直接写，
// 连混合都省掉。

type roundMask struct {
	w, h, r int
	cov     []uint8 // 覆盖率 0..255，行优先
	fullL   []int16 // 每行首个满覆盖 x；-1 表示该行没有满覆盖像素
	fullR   []int16 // 每行末个满覆盖 x
}

var (
	rrMu    sync.Mutex
	rrCache = map[uint64]*roundMask{}
	// 屏幕上卡片尺寸就那几种，缓存几十张足够；一旦超了整体清空，
	// 避免"页面高度随数据变化"之类的情况把内存顶上去。
	rrCacheMax = 64
)

func (m *roundMask) buildRows() {
	m.fullL = make([]int16, m.h)
	m.fullR = make([]int16, m.h)
	for y := 0; y < m.h; y++ {
		row := y * m.w
		l, r := int16(-1), int16(-1)
		for x := 0; x < m.w; x++ {
			if m.cov[row+x] == 255 {
				if l < 0 {
					l = int16(x)
				}
				r = int16(x)
			}
		}
		m.fullL[y], m.fullR[y] = l, r
	}
}

func roundMaskFor(w, h, r int) *roundMask {
	if w <= 0 || h <= 0 {
		return nil
	}
	if 2*r > w {
		r = w / 2
	}
	if 2*r > h {
		r = h / 2
	}
	if r < 0 {
		r = 0
	}
	key := uint64(w)*1000000 + uint64(h)*1000 + uint64(r)

	rrMu.Lock()
	if m, ok := rrCache[key]; ok {
		rrMu.Unlock()
		return m
	}
	rrMu.Unlock()

	m := &roundMask{w: w, h: h, r: r, cov: make([]uint8, w*h)}
	fw, fh, fr := float64(w), float64(h), float64(r)
	for y := 0; y < h; y++ {
		row := y * w
		py := float64(y) + 0.5
		for x := 0; x < w; x++ {
			d := sdRoundRect(float64(x)+0.5, py, 0, 0, fw, fh, fr)
			cov := 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			m.cov[row+x] = uint8(cov*255 + 0.5)
		}
	}
	m.buildRows()

	rrMu.Lock()
	if len(rrCache) >= rrCacheMax {
		rrCache = map[uint64]*roundMask{}
	}
	rrCache[key] = m
	rrMu.Unlock()
	return m
}

const inv255 = 1.0 / 255.0

// drawMasked 把 col 按遮罩刷到画布上，(x,y) 对应遮罩的 (0,0)。
//
// 分三段走：左过渡 / 中间满覆盖 / 右过渡。中间段的覆盖率恒为 255，
// 不透明色时直接写像素（省掉混合的浮点运算），这是卡片这种大面积元素
// 能快下来的关键。
func (c *Canvas) drawMasked(m *roundMask, x, y int, col RGBA, rowAlpha func(int) float64) {
	if m == nil {
		return
	}
	x0, x1 := c.clampX(x, x+m.w-1)
	y0, y1 := c.clampY(y, y+m.h-1)
	if x0 < x {
		x0 = x
	}
	if x1 > x+m.w-1 {
		x1 = x + m.w - 1
	}
	if x0 > x1 || y0 > y1 {
		return
	}
	fast := col.A == 255 && c.ga >= 1 && rowAlpha == nil
	for yy := y0; yy <= y1; yy++ {
		ry := yy - y
		ra := 1.0
		if rowAlpha != nil {
			ra = rowAlpha(ry)
			if ra <= 0 {
				continue
			}
		}
		row := ry * m.w
		l, rg := m.fullL[ry], m.fullR[ry]
		if l < 0 {
			for xx := x0; xx <= x1; xx++ {
				if cov := m.cov[row+xx-x]; cov != 0 {
					c.Blend(xx, yy, col, float64(cov)*inv255*ra)
				}
			}
			continue
		}
		exL := x + int(l) - 1
		if exL > x1 {
			exL = x1
		}
		for xx := x0; xx <= exL; xx++ {
			if cov := m.cov[row+xx-x]; cov != 0 {
				c.Blend(xx, yy, col, float64(cov)*inv255*ra)
			}
		}
		sx := x + int(l)
		if sx < x0 {
			sx = x0
		}
		exR := x + int(rg)
		if exR > x1 {
			exR = x1
		}
		if fast {
			for xx := sx; xx <= exR; xx++ {
				i := (yy*c.W + xx) << 2
				c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = col.R, col.G, col.B, 255
			}
		} else {
			for xx := sx; xx <= exR; xx++ {
				c.Blend(xx, yy, col, float64(m.cov[row+xx-x])*inv255*ra)
			}
		}
		sxR := x + int(rg) + 1
		if sxR < x0 {
			sxR = x0
		}
		for xx := sxR; xx <= x1; xx++ {
			if cov := m.cov[row+xx-x]; cov != 0 {
				c.Blend(xx, yy, col, float64(cov)*inv255*ra)
			}
		}
	}
}

// FillRoundRect 抗锯齿圆角矩形
func (c *Canvas) FillRoundRect(x, y, w, h, r int, col RGBA) {
	if w <= 0 || h <= 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	c.drawMasked(roundMaskFor(w, h, r), x, y, col, nil)
}

// strokeMaskFor 描边遮罩。描边的形状 = 沿矩形边界内外各 half 的环带，
// 遍历范围比矩形本身大 t+1，所以遮罩尺寸是 (w+2t+3)×(h+2t+3)，
// 遮罩原点对应屏幕上的 (x-t-1, y-t-1)。
var (
	rsMu    sync.Mutex
	rsCache = map[uint64]*roundMask{}
)

func strokeMaskFor(w, h, r, t int) *roundMask {
	if w <= 0 || h <= 0 || t <= 0 {
		return nil
	}
	// 屏幕尺寸上限已知（w≤376, h≤960, r/t 都是个位数），直接拼十进制 key 最省事
	key := uint64(w)*1_000_000_000 + uint64(h)*1_000_000 + uint64(r)*1000 + uint64(t)

	rsMu.Lock()
	if m, ok := rsCache[key]; ok {
		rsMu.Unlock()
		return m
	}
	rsMu.Unlock()

	mw, mh := w+2*t+3, h+2*t+3
	m := &roundMask{w: mw, h: mh, r: r, cov: make([]uint8, mw*mh)}
	half := float64(t) / 2
	fw, fh, fr := float64(w), float64(h), float64(r)
	for my := 0; my < mh; my++ {
		row := my * mw
		py := float64(my-t-1) + 0.5
		for mx := 0; mx < mw; mx++ {
			px := float64(mx-t-1) + 0.5
			d := math.Abs(sdRoundRect(px, py, 0, 0, fw, fh, fr))
			cov := half + 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			m.cov[row+mx] = uint8(cov*255 + 0.5)
		}
	}
	m.buildRows()

	rsMu.Lock()
	if len(rsCache) >= rrCacheMax {
		rsCache = map[uint64]*roundMask{}
	}
	rsCache[key] = m
	rsMu.Unlock()
	return m
}

// StrokeRoundRect 圆角矩形描边（厚度 t，居中于边界）
func (c *Canvas) StrokeRoundRect(x, y, w, h, r, t int, col RGBA) {
	if w <= 0 || h <= 0 || t <= 0 {
		return
	}
	m := strokeMaskFor(w, h, r, t)
	if m == nil {
		return
	}
	c.drawMasked(m, x-t-1, y-t-1, col, nil)
}

// ------------------------------------------------------------
//  圆 / 圆弧
// ------------------------------------------------------------

func (c *Canvas) FillCircle(cx, cy, r float64, col RGBA) {
	r2 := r + 1
	x0, x1 := int(cx-r2), int(cx+r2)
	y0, y1 := int(cy-r2), int(cy+r2)
	if !c.clipVisible(x0, y0, x1-x0+1, y1-y0+1) {
		return
	}
	x0, x1 = c.clampX(x0, x1)
	y0, y1 = c.clampY(y0, y1)
	for yy := y0; yy <= y1; yy++ {
		for xx := x0; xx <= x1; xx++ {
			d := math.Hypot(float64(xx)+0.5-cx, float64(yy)+0.5-cy)
			cov := r + 0.5 - d
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			c.Blend(xx, yy, col, cov)
		}
	}
}

// Arc 抗锯齿圆环扇区。角度以弧度计，0 = 12 点方向，顺时针为正。
func (c *Canvas) Arc(cx, cy, rOut, rIn, aStart, aSweep float64, col RGBA) {
	if aSweep <= 0 || rOut <= rIn {
		return
	}
	if aSweep > 2*math.Pi {
		aSweep = 2 * math.Pi
	}
	aEnd := aStart + aSweep
	outer := rOut + 1
	x0, x1 := int(cx-outer), int(cx+outer)
	y0, y1 := int(cy-outer), int(cy+outer)
	if !c.clipVisible(x0, y0, x1-x0+1, y1-y0+1) {
		return
	}
	x0, x1 = c.clampX(x0, x1)
	y0, y1 = c.clampY(y0, y1)
	for yy := y0; yy <= y1; yy++ {
		dy := float64(yy) + 0.5 - cy
		for xx := x0; xx <= x1; xx++ {
			dx := float64(xx) + 0.5 - cx
			r := math.Hypot(dx, dy)
			// 半径方向覆盖率
			var cov float64
			if r <= rIn-0.5 || r >= rOut+0.5 {
				continue
			}
			if r < rIn {
				cov = rIn - r
			} else if r > rOut {
				cov = rOut - r
			} else {
				cov = 1
			}
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			// 角度方向
			a := math.Atan2(dx, -dy)
			if a < 0 {
				a += 2 * math.Pi
			}
			if a < aStart || a > aEnd {
				continue
			}
			// 角度边缘 AA（1 像素弧长对应的角度）
			edge := 0.5 / math.Max(r, 1)
			if a-aStart < edge {
				cov *= (a - aStart) / edge
			}
			if aEnd-a < edge {
				cov *= (aEnd - a) / edge
			}
			c.Blend(xx, yy, col, cov)
		}
	}
}

// ------------------------------------------------------------
//  线
// ------------------------------------------------------------

// Line 抗锯齿粗线
func (c *Canvas) Line(x0, y0, x1, y1, thick float64, col RGBA) {
	dx, dy := x1-x0, y1-y0
	length := math.Hypot(dx, dy)
	if length < 0.0001 {
		c.FillCircle(x0, y0, thick/2, col)
		return
	}
	half := thick / 2
	steps := int(length*2) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		px := x0 + dx*t
		py := y0 + dy*t
		c.FillCircle(px, py, half, col)
	}
}

// ------------------------------------------------------------
//  环形进度遮罩（预计算角度，避免每帧 atan2）
// ------------------------------------------------------------

type RingMask struct {
	BX, BY, BW, BH int
	ang            []float32 // 归一化角度 0..1（12点顺时针），-1 表示不在环上
	cov            []float32 // 半径方向覆盖率
}

// NewRingMask 预计算一个以原点为中心的圆环角度/覆盖率表
// 以原点为中心意味着同一半径的遮罩可以跨位置、跨帧复用
func NewRingMask(rOut, rIn float64) *RingMask {
	half := int(math.Ceil(rOut)) + 2
	bx, by := -half, -half
	bw := half*2 + 1
	bh := bw
	m := &RingMask{BX: bx, BY: by, BW: bw, BH: bh}
	m.ang = make([]float32, bw*bh)
	m.cov = make([]float32, bw*bh)
	for j := 0; j < bh; j++ {
		dy := float64(by+j) + 0.5
		for i := 0; i < bw; i++ {
			dx := float64(bx+i) + 0.5
			idx := j*bw + i
			r := math.Hypot(dx, dy)
			var cov float64
			switch {
			case r < rIn-0.5 || r > rOut+0.5:
				m.ang[idx] = -1
				continue
			case r < rIn:
				cov = rIn - r
			case r > rOut:
				cov = rOut - r
			default:
				cov = 1
			}
			if cov <= 0 {
				m.ang[idx] = -1
				continue
			}
			if cov > 1 {
				cov = 1
			}
			a := math.Atan2(dx, -dy)
			if a < 0 {
				a += 2 * math.Pi
			}
			m.ang[idx] = float32(a / (2 * math.Pi))
			m.cov[idx] = float32(cov)
		}
	}
	return m
}

// DrawRingAt 在 (cx,cy) 处绘制 [from,to] 归一化角度区间的环
// 角度 0 = 12 点方向，顺时针为正。
// from/to 允许越过 1（例如 0.625 → 1.375），用于绘制下方开口的仪表弧。
func (c *Canvas) DrawRingAt(m *RingMask, cx, cy int, from, to float64, col RGBA) {
	if m == nil || to <= from {
		return
	}
	if !c.clipVisible(cx+m.BX, cy+m.BY, m.BW, m.BH) {
		return
	}
	// 有效的列索引范围（i 是相对 m.BX 的偏移）
	i0, i1 := c.clampX(cx+m.BX, cx+m.BX+m.BW-1)
	i0 -= cx + m.BX
	i1 -= cx + m.BX
	if i0 < 0 {
		i0 = 0
	}
	if i1 >= m.BW {
		i1 = m.BW - 1
	}
	for j := 0; j < m.BH; j++ {
		py := cy + m.BY + j
		if py < c.clipMinY() || py > c.clipMaxY()-1 {
			continue
		}
		row := j * m.BW
		for i := i0; i <= i1; i++ {
			idx := row + i
			af := float64(m.ang[idx])
			if af < 0 {
				continue
			}
			a := af
			if a < from {
				a += 1
			}
			if a < from || a > to {
				continue
			}
			c.Blend(cx+m.BX+i, py, col, float64(m.cov[idx]))
		}
	}
}

// FillRoundRectFade 圆角矩形，透明度从顶部在 fadeH 高度内衰减到 0（顶部高光）
func (c *Canvas) FillRoundRectFade(x, y, w, h, r int, col RGBA, fadeH int) {
	if w <= 0 || h <= 0 || fadeH <= 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	m := roundMaskFor(w, h, r)
	if m == nil {
		return
	}
	base := float64(col.A) / 255.0
	c.drawMasked(m, x, y, RGBA{col.R, col.G, col.B, 255}, func(ry int) float64 {
		if ry >= fadeH {
			return 0
		}
		t := 1 - float64(ry)/float64(fadeH)
		return base * t * t
	})
}

// ------------------------------------------------------------
//  图表
// ------------------------------------------------------------

// AreaChart 面积折线图。data 为历史采样（最新在右），maxV <= 0 时自动取峰值。
func (c *Canvas) AreaChart(x, y, w, h int, data []float64, maxV float64, window int, lineCol, fillCol RGBA, thick float64) {
	if w <= 1 || h <= 1 || len(data) == 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	// 数据点少于窗口时铺满整个宽度（避免右侧一小块、左侧大片空白）
	if window < 2 || len(data) < window {
		window = len(data)
	}
	if window < 2 {
		return
	}
	if maxV <= 0 {
		for _, v := range data {
			if v > maxV {
				maxV = v
			}
		}
		if maxV <= 0 {
			maxV = 1
		}
	}
	n := len(data)
	start := window - n
	if start < 0 {
		start = 0
	}
	baseY := y + h - 1

	// 水平网格：只留 2 条，极淡
	gx0, gx1 := c.clampX(x, x+w-1)
	gridCol := WithA(lineCol, 14)
	for i := 1; i <= 2; i++ {
		gy := y + h - int(float64(h)*float64(i)/3.0+0.5)
		if gy < c.clipMinY() || gy > c.clipMaxY()-1 {
			continue
		}
		for gx := gx0; gx <= gx1; gx += 4 {
			c.Blend(gx, gy, gridCol, 1)
			c.Blend(gx+1, gy, gridCol, 1)
		}
	}

	// 逐列插值绘制
	for col := gx0 - x; col <= gx1-x; col++ {
		t := float64(col) / float64(w-1) * float64(window-1)
		idx := int(t)
		rel := float64(idx) - float64(start)
		if rel < -1 || rel+1 > float64(n) {
			continue
		}
		i0 := int(rel)
		i1 := i0 + 1
		if i0 < 0 {
			i0 = 0
		}
		if i1 >= n {
			i1 = n - 1
		}
		if i0 >= n {
			continue
		}
		f := t - float64(idx)
		v := data[i0]*(1-f) + data[i1]*f
		if v < 0 {
			v = 0
		}
		if v > maxV {
			v = maxV
		}
		py := baseY - int(v/maxV*float64(h-1)+0.5)
		if py < y {
			py = y
		}
		px := x + col
		// 面积填充：自上而下快速淡出，保持克制
		fy0, fy1 := c.clampY(py, baseY)
		for yy := fy0; yy <= fy1; yy++ {
			tt := float64(yy-py) / math.Max(float64(baseY-py), 1)
			a := 0.26 * (1 - tt*0.9)
			c.Blend(px, yy, fillCol, a)
		}
		// 折线
		c.Blend(px, py, lineCol, 1)
		if thick >= 1.5 {
			c.Blend(px, py-1, lineCol, 0.5)
			c.Blend(px, py+1, lineCol, 0.3)
		}
	}

	// 基线
	for gx := gx0; gx <= gx1; gx++ {
		c.Blend(gx, baseY, WithA(lineCol, 46), 1)
	}

	// 最新点：小实心点，不做光晕
	last := n - 1
	lv := data[last]
	if lv < 0 {
		lv = 0
	}
	if lv > maxV {
		lv = maxV
	}
	lt := 0.0
	if window > 1 {
		lt = float64(start+last) / float64(window-1)
	}
	lx := float64(x) + lt*float64(w-1)
	ly := float64(baseY) - lv/maxV*float64(h-1)
	c.FillCircle(lx, ly, 2.8, lineCol)
}

// Curve 单条曲线（可选面积填充），用于在**同一个坐标系**里叠加多条曲线。
//
// 与 AreaChart 的差别：不画网格、不画基线、不画端点 —— 这些属于"整张图"的
// 装饰，画两遍就会重影。首页网络卡的「↑上传 / ↓下载」就是两条共坐标的曲线：
// 先把带面积的一条画上，再把另一条（fillA=0，只画线）叠上去。
func (c *Canvas) Curve(x, y, w, h int, data []float64, maxV float64, window int,
	lineCol RGBA, thick float64, fillA float64) {

	if w <= 1 || h <= 1 || len(data) == 0 || !c.clipVisible(x, y, w, h) {
		return
	}
	if window < 2 || len(data) < window {
		window = len(data)
	}
	if window < 2 {
		return
	}
	if maxV <= 0 {
		for _, v := range data {
			if v > maxV {
				maxV = v
			}
		}
		if maxV <= 0 {
			maxV = 1
		}
	}
	n := len(data)
	start := window - n
	if start < 0 {
		start = 0
	}
	baseY := y + h - 1

	cx0, cx1 := c.clampX(x, x+w-1)
	for col := cx0 - x; col <= cx1-x; col++ {
		t := float64(col) / float64(w-1) * float64(window-1)
		idx := int(t)
		rel := float64(idx) - float64(start)
		if rel < -1 || rel+1 > float64(n) {
			continue
		}
		i0 := int(rel)
		i1 := i0 + 1
		if i0 < 0 {
			i0 = 0
		}
		if i1 >= n {
			i1 = n - 1
		}
		if i0 >= n {
			continue
		}
		f := t - float64(idx)
		v := data[i0]*(1-f) + data[i1]*f
		if v < 0 {
			v = 0
		}
		if v > maxV {
			v = maxV
		}
		py := baseY - int(v/maxV*float64(h-1)+0.5)
		if py < y {
			py = y
		}
		px := x + col
		if fillA > 0 {
			// 面积：自上而下淡出（贴近参考卡片里那种"从线往下化开"的面）
			fy0, fy1 := c.clampY(py, baseY)
			for yy := fy0; yy <= fy1; yy++ {
				tt := float64(yy-py) / math.Max(float64(baseY-py), 1)
				c.Blend(px, yy, lineCol, fillA*(1-tt*0.85))
			}
		}
		c.Blend(px, py, lineCol, 1)
		if thick >= 1.5 {
			c.Blend(px, py-1, lineCol, 0.5)
			c.Blend(px, py+1, lineCol, 0.3)
		}
	}

	// 端点小实心点（只给带面积的主曲线，避免两条线各戳一个点）
	if fillA <= 0 {
		return
	}
	last := n - 1
	lv := data[last]
	if lv < 0 {
		lv = 0
	}
	if lv > maxV {
		lv = maxV
	}
	lt := 0.0
	if window > 1 {
		lt = float64(start+last) / float64(window-1)
	}
	lx := float64(x) + lt*float64(w-1)
	ly := float64(baseY) - lv/maxV*float64(h-1)
	c.FillCircle(lx, ly, 2.6, lineCol)
}

// ------------------------------------------------------------
//  Sprite —— 离屏小图，用于缓存静态元素
// ------------------------------------------------------------

type Sprite struct {
	W, H int
	Pix  []byte
}

// Snapshot 把画布的一个区域截取为 Sprite
func (c *Canvas) Snapshot(x, y, w, h int) *Sprite {
	sp := &Sprite{W: w, H: h, Pix: make([]byte, w*h*4)}
	for j := 0; j < h; j++ {
		src := ((y+j)*c.W + x) << 2
		dst := (j * w) << 2
		copy(sp.Pix[dst:dst+w*4], c.Pix[src:src+w*4])
	}
	return sp
}

// DrawSprite 在 (x,y) 绘制 Sprite（左上角对齐）
func (c *Canvas) DrawSprite(s *Sprite, x, y int) {
	if s == nil || !c.clipVisible(x, y, s.W, s.H) {
		return
	}
	gx0, gx1 := c.clampX(x, x+s.W-1)
	sy0, _ := c.clampY(y, y+s.H-1)
	for j := sy0 - y; j < s.H; j++ {
		py := y + j
		if py < c.clipMinY() || py > c.clipMaxY()-1 {
			continue
		}
		row := j * s.W * 4
		for i := gx0 - x; i <= gx1-x; i++ {
			k := row + i*4
			a := s.Pix[k+3]
			if a == 0 {
				continue
			}
			c.Blend(x+i, py, RGBA{s.Pix[k], s.Pix[k+1], s.Pix[k+2], 255}, float64(a)/255)
		}
	}
}

// BlitOpaque 把 Sprite **不透明地**拷到 (x,y)，整行 memcpy。
//
// 只用于已确认不透明的位图（页面快照：卡片都画在不透明的背景底图之上，
// alpha 全 255）。与 DrawSprite 的差别是省掉了逐像素 alpha 混合，
// 快一个量级 —— 页面滑动每帧要搬两屏位图，这里不能含糊。
func (c *Canvas) BlitOpaque(s *Sprite, x, y int) {
	if s == nil || s.W <= 0 || s.H <= 0 {
		return
	}
	dx0, dx1 := x, x+s.W
	if dx0 < 0 {
		dx0 = 0
	}
	if dx1 > c.W {
		dx1 = c.W
	}
	if dx0 >= dx1 {
		return
	}
	dy0, dy1 := y, y+s.H
	if dy0 < 0 {
		dy0 = 0
	}
	if dy1 > c.H {
		dy1 = c.H
	}
	if dy0 >= dy1 {
		return
	}
	n := (dx1 - dx0) * 4
	sx := dx0 - x
	for j := dy0; j < dy1; j++ {
		src := ((j-y)*s.W + sx) * 4
		dst := (j*c.W + dx0) * 4
		copy(c.Pix[dst:dst+n], s.Pix[src:src+n])
	}
}

// ------------------------------------------------------------
//  工具
// ------------------------------------------------------------

// Bar 圆角进度条
func (c *Canvas) Bar(x, y, w, h, r int, pct float64, track, fill RGBA) {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	c.FillRoundRect(x, y, w, h, r, track)
	fw := int(float64(w)*pct + 0.5)
	if fw <= 0 {
		return
	}
	if fw < h {
		fw = h
	}
	c.FillRoundRect(x, y, fw, h, r, fill)
}
