package ui

import "math"

// ============================================================
//  天气图形（彩色）
//
//  「彩色」是洋哥的明确要求 —— 天气图标是全屏唯一允许出现多色的区域。
//  但仍守住高级感的底线：
//   - 低饱和（不是纯色，都掺了灰）、不发光、不霓虹
//   - 颜色只用来「描述天气本身」：太阳金 / 雨蓝 / 雪白 / 雾灰 / 霾土黄
//   - 线条仍是细的，构图仍是几何的，不用卡通描边
// ============================================================

// 天气调色板：固定色，在四套深色主题上都协调（不随主题变色，保证识别度）
var (
	wxSunHi    = C(243, 193, 105) // 太阳亮面（暖金）
	wxSunLo    = C(206, 146, 66)  // 太阳暗面 / 光芒
	wxCloudTop = C(221, 229, 237) // 云顶（偏白）
	wxCloudBot = C(133, 146, 162) // 云底（偏灰蓝）
	wxRain     = C(115, 167, 216) // 雨（蓝）
	wxSnow     = C(223, 237, 248) // 雪（近白）
	wxFog      = C(157, 171, 184) // 雾（灰蓝）
	wxHaze     = C(180, 166, 146) // 霾（土黄）
	wxBolt     = C(250, 208, 106) // 闪电（金）
)

// ------------------------------------------------------------
//  动画基础：让运动"有生命"
// ------------------------------------------------------------
//
// 上一版的相位全是线性的（t*0.45 / t*0.95 / t*0.42 …），而且往复运动直接用单频
// 正弦。线性相位 = 匀速刚体运动，单频正弦 = 整块同步摆动，两者叠加就是"生硬"。
//
// 这一版只做三件事，但足以把观感拉开：
//   1. 相位传播 —— 不是整个图形一起动，而是"波的峰"绕着/沿着图形走；
//   2. 多频叠加 —— 让周期不可察觉（wobble）；
//   3. 分层相位 —— 每个元素有自己的频率，绝不与邻居共享节拍。

// easeIO 缓入缓出（0→1）。任何"从 A 到 B"位移都该经过它，
// 否则一动一停就是机械感。
func easeIO(x float64) float64 {
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	return 0.5 - 0.5*math.Cos(x*math.Pi)
}

// wobble 三频叠加的有机摆动，值域约 [-1,1]。
// 三个频率的比值取了无理数附近的数（0.47 / 1.73），叠加后周期极长，
// 看不出"来回"这个动作本身 —— 这是"飘"与"晃"的分界。
func wobble(t, p float64) float64 {
	return math.Sin(t*p)*0.64 +
		math.Sin(t*p*0.47+1.31)*0.24 +
		math.Sin(t*p*1.73+2.77)*0.12
}

// fadeLine 两端渐隐的水平线。雾带、光带都不该有生硬的端点。
// 逐像素 Blend（而不是叠一串小圆），避免 alpha 在重叠处累加。
func fadeLine(c *Canvas, x0, x1, y, thick float64, col RGBA, aMax uint8) {
	n := int(x1-x0) + 1
	if n < 3 {
		return
	}
	for i := 0; i < n; i++ {
		u := float64(i) / float64(n-1)
		e := math.Sin(u * math.Pi)
		a := float64(aMax) / 255 * e
		c.Blend(int(x0)+i, int(y), col, a)
		if thick > 1.4 {
			c.Blend(int(x0)+i, int(y)+1, col, a*0.5)
		}
	}
}

// weatherAccent 取该天气类型的「代表色」，供卡片描边等点缀使用。
// 与图标调色板同源 —— 这样整张天气卡的色相是统一的，
// 雨天卡片泛蓝、晴天卡片泛金，一眼就能对上。
func weatherAccent(kind string) RGBA {
	switch kind {
	case "sun":
		return wxSunHi
	case "cloud":
		return wxSunHi // 多云：云缝里露着太阳，取暖金
	case "rain", "thunder":
		return wxRain
	case "snow":
		return wxSnow
	case "fog":
		return wxFog
	default: // overcast
		return wxCloudBot
	}
}

// DrawWeatherIcon 在 (cx,cy) 处以半径 r 绘制天气图形。
// kind: sun / cloud / overcast / rain / snow / fog / thunder
// t 为时间（秒），驱动全部动画。
func DrawWeatherIcon(c *Canvas, cx, cy int, r float64, kind string, t float64) {
	fx, fy := float64(cx), float64(cy)
	switch kind {
	case "sun":
		drawSun(c, fx, fy, r, t)
	case "cloud":
		drawSunCloud(c, fx, fy, r, t)
	case "rain":
		drawRain(c, fx, fy, r, t)
	case "snow":
		drawSnow(c, fx, fy, r, t)
	case "fog":
		drawFog(c, fx, fy, r, t)
	case "thunder":
		drawThunder(c, fx, fy, r, t)
	default: // overcast 及未知
		drawOvercast(c, fx, fy, r, t)
	}
}

// ---- 云形状掩码 ----
//
// 直接用若干半透明圆叠加，交叠处会叠加两次而发亮、还会露出底座直角。
// 这里先把形状合成到一张覆盖率掩码（取较大值，不累加），再整朵一次性混合。

type cloudMask struct {
	w, h int   // 有效尺寸
	pw   int   // 行宽（左右各留一列哨兵）
	buf  []uint8
}

// idx 把掩码坐标映射到带哨兵的缓冲区索引。
//
// 四周各留一圈值为 0 的哨兵：双线性重采样会取到 i0=-1 或 j0=h 这样的
// 越界位置，有了哨兵就能自动得到 0 而**不必逐像素做边界判断** ——
// 这是把重采样写成紧凑循环的前提（每帧 90×60 个像素，多一个分支就多一份开销）。
func (m *cloudMask) idx(i, j int) int { return (j+1)*m.pw + (i + 1) }

var cloudMaskCache = map[int]*cloudMask{}

func getCloudMask(s float64) *cloudMask {
	key := int(s*8 + 0.5)
	if m, ok := cloudMaskCache[key]; ok {
		return m
	}
	w := int(1.30*s) + 4
	h := int(0.82*s) + 4
	pw := w + 2
	m := &cloudMask{w: w, h: h, pw: pw, buf: make([]uint8, pw*(h+2))}

	// 世界坐标 → 掩码坐标
	lx := func(v float64) float64 { return v + 0.60*s }
	ly := func(v float64) float64 { return v + 0.44*s }

	// 三团圆冠
	maskDisc(m, lx(-0.30*s), ly(0.02*s), 0.245*s)
	maskDisc(m, lx(0.05*s), ly(-0.11*s), 0.300*s)
	maskDisc(m, lx(0.35*s), ly(0.05*s), 0.225*s)
	// 圆角底座：端部为半圆，与圆冠弧线自然衔接，不出现直角
	maskRoundRect(m, lx(-0.50*s), ly(0.06*s), 1.00*s, 0.19*s, 0.095*s)

	cloudMaskCache[key] = m
	return m
}

// maskDisc 用覆盖率写入掩码（取较大值，交叠处不会被叠加变亮）
func maskDisc(m *cloudMask, cx, cy, r float64) {
	x0, x1 := int(cx-r-1), int(cx+r+1)
	y0, y1 := int(cy-r-1), int(cy+r+1)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 >= m.w {
		x1 = m.w - 1
	}
	if y1 >= m.h {
		y1 = m.h - 1
	}
	for y := y0; y <= y1; y++ {
		dy := float64(y) + 0.5 - cy
		for x := x0; x <= x1; x++ {
			dx := float64(x) + 0.5 - cx
			cov := r + 0.5 - math.Hypot(dx, dy)
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			i := m.idx(x, y)
			if v := uint8(cov*255 + 0.5); v > m.buf[i] {
				m.buf[i] = v
			}
		}
	}
}

// maskRoundRect 圆角矩形掩码；r 取高度一半时端部即半圆
func maskRoundRect(m *cloudMask, x, y, w, h, r float64) {
	if w < 2*r {
		r = w / 2
	}
	if h < 2*r {
		r = h / 2
	}
	for yy := int(y - 1); yy <= int(y+h+1); yy++ {
		if yy < 0 || yy >= m.h {
			continue
		}
		for xx := int(x - 1); xx <= int(x+w+1); xx++ {
			if xx < 0 || xx >= m.w {
				continue
			}
			cov := 0.5 - sdRoundRect(float64(xx)+0.5, float64(yy)+0.5, x, y, w, h, r)
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			i := m.idx(xx, yy)
			if v := uint8(cov*255 + 0.5); v > m.buf[i] {
				m.buf[i] = v
			}
		}
	}
}

// cloudBody 整朵云一次性绘制。
// 颜色沿纵向从「云顶亮白」渐变到「云底灰蓝」，让云有体积感 —— 这是彩色化的关键一步。
//
// 🔴 亚像素定位。
//
// 掩码是按整数像素栅格化的，旧代码把左上角直接 `int(cx - 0.60*s)`：
// 云的漂移速度实测约 2.2px/s，于是每一帧算出的新坐标有十几帧取整到同一个
// 像素 —— 云"卡住"不动，然后**跳 1 个像素**。这就是"生硬"的像素级来源：
// 它跟缓动曲线、跟帧数都无关，是纯粹的坐标量化。
//
// 这条 bug 还锁死了性能优化的方向：帧率越低，每次跳的幅度越大，
// 所以"降帧率省 CPU"会直接把观感做坏。做成亚像素之后，
// 云在 12fps 下（每帧 0.18px）反而比原来的 30fps 整数跳变更平滑。
//
// 实现：双线性重采样。x 方向的取样下标与权重对每一行都一样，先算一次。
// 每像素多 3 次采样，掩码只有约 90×60，代价可忽略。
func cloudBody(c *Canvas, cx, cy, s, alpha float64) {
	if s < 4 || alpha <= 0 {
		return
	}
	m := getCloudMask(s)
	ox, oy := cx-0.60*s, cy-0.44*s
	inv := 0.0
	if m.h > 1 {
		inv = 1 / float64(m.h-1)
	}
	gx, gy := int(math.Floor(ox)), int(math.Floor(oy))
	fx, fy := ox-float64(gx), oy-float64(gy)

	// 掩码宽到超出定长权重表时退回整数定位：宁可丢一点平滑，也不能越界。
	if m.w > maxMaskW {
		for j := 0; j < m.h; j++ {
			col := Lerp(wxCloudTop, wxCloudBot, float64(j)*inv)
			for i := 0; i < m.w; i++ {
				if v := m.buf[m.idx(i, j)]; v != 0 {
					c.Blend(gx+i, gy+j, col, float64(v)/255*alpha)
				}
			}
		}
		return
	}

	// x 方向：把每个输出列要取的两个掩码下标与权重预先算好
	var xi [maxMaskW]int
	var xw [maxMaskW]float64
	for i := 0; i < m.w; i++ {
		t := float64(i) - fx
		a := int(math.Floor(t))
		xi[i] = a
		xw[i] = t - float64(a)
	}

	for j := 0; j < m.h; j++ {
		ty := float64(j) - fy
		ry := int(math.Floor(ty)) // 可能是 -1（哨兵行，取到 0）
		wy := ty - float64(ry)
		by := 1 - wy
		// 两行的起始下标（已含哨兵偏移）
		ra := (ry+1)*m.pw + 1
		rb := (ry+2)*m.pw + 1
		col := Lerp(wxCloudTop, wxCloudBot, float64(j)*inv)
		py := gy + j
		for i := 0; i < m.w; i++ {
			a := xi[i]
			wxi := xw[i]
			bxi := 1 - wxi
			v := float64(m.buf[ra+a])*bxi*by +
				float64(m.buf[ra+a+1])*wxi*by +
				float64(m.buf[rb+a])*bxi*wy +
				float64(m.buf[rb+a+1])*wxi*wy
			if v < 0.5 {
				continue
			}
			c.Blend(gx+i, py, col, v/255*alpha)
		}
	}
}

// maxMaskW 定长权重表容量。云掩码实际约 90 列（图标半径 48），
// 留到 256 是给"以后把图标画得更大"的余量。
const maxMaskW = 256

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// sunDisc 金色太阳圆盘：外圈柔光 + 亮面 + 略偏移的高光
func sunDisc(c *Canvas, cx, cy, r float64) {
	c.FillCircle(cx, cy, r, WithA(wxSunLo, 64))
	c.FillCircle(cx, cy, r*0.78, wxSunLo)
	c.FillCircle(cx-r*0.16, cy-r*0.18, r*0.60, wxSunHi)
}

// ---- 晴：金色圆盘 + 沿圆周流动的光芒 ----
//
// 旧版是 8 条光芒**整体匀速旋转** —— 轮廓在转，像风扇叶片，这是最廉价的一种
// 太阳画法。现在轮廓不动，改成「亮度与长度的波峰绕圆周缓慢传播」：
// 光在流动，形不变。每根光芒的变化周期约 1.7 秒，整圈 13 秒。
func drawSun(c *Canvas, cx, cy, r, t float64) {
	breathe := 1 + 0.035*math.Sin(t*0.92)
	sunDisc(c, cx, cy, r*0.42*breathe)

	for i := 0; i < 8; i++ {
		// 波峰位置 = 该根的角度 - 传播距离
		ph := float64(i)/8 - t*0.075
		wv := 0.5 + 0.5*math.Sin(ph*2*math.Pi)
		a := float64(i) * math.Pi / 4
		sin, cos := math.Sin(a), math.Cos(a)
		l0 := r * 0.61
		l1 := r * (0.78 + 0.055*wv)
		c.Line(cx+l0*sin, cy-l0*cos, cx+l1*sin, cy-l1*cos, 2.2,
			WithA(wxSunLo, clampByte((0.48+0.47*wv)*255)))
	}
}

// ---- 多云：右上露出太阳 + 云缓慢漂移 ----
func drawSunCloud(c *Canvas, cx, cy, r, t float64) {
	scx, scy := cx+r*0.34, cy-r*0.36
	sunDisc(c, scx, scy, r*0.22*(1+0.045*math.Sin(t*0.72)))
	for i := 0; i < 6; i++ {
		ph := float64(i)/6 - t*0.115
		wv := 0.5 + 0.5*math.Sin(ph*2*math.Pi)
		a := float64(i) * math.Pi / 3
		sin, cos := math.Sin(a), math.Cos(a)
		l0 := r * 0.30
		l1 := r * (0.40 + 0.032*wv)
		c.Line(scx+l0*sin, scy-l0*cos, scx+l1*sin, scy-l1*cos, 2.0,
			WithA(wxSunLo, clampByte((0.48+0.47*wv)*255)))
	}
	// 云不再整朵刚性平移：三频漂移 + 极缓的呼吸缩放。
	// 频率取 0.46 —— 0.30 时整朵云只有 1px/s 的位移，肉眼几乎读不出"在动"，
	// 那就等于没做动画；0.46 下约 1.8px/s，慢，但看得出是在飘。
	off := wobble(t, 0.46) * r * 0.105
	sc := 1 + 0.020*math.Sin(t*0.52)
	cloudBody(c, cx+off, cy+r*0.12, r*1.34*sc, 0.90)
}

// ---- 阴：两层云各走各的节拍 ----
func drawOvercast(c *Canvas, cx, cy, r, t float64) {
	// 旧版两层共用 sin(t*0.35) 与 sin(t*0.35*1.35) —— 频率成简单倍数，
	// 会周期性"撞在一起"同步摆动。现在两个频率互不相干。
	o1 := wobble(t, 0.34) * r * 0.070
	o2 := wobble(t, 0.48) * r * 0.100
	cloudBody(c, cx+o1-r*0.12, cy-r*0.12, r*1.12, 0.52)
	cloudBody(c, cx+o2+r*0.14, cy+r*0.18, r*1.34, 0.92)
}

// ---- 雨：云 + 加速下落的雨丝 ----
//
// 三处修正：
//   · 相位均匀错开（旧版步长 0.27×4 ≈ 1.08，第 5 滴几乎与第 1 滴重合）；
//   · 位移用平方加速 —— "重力"与"匀速平移"的分界线就在这一行；
//   · 雨丝长度随速度增长，快的时候更长。
func drawRain(c *Canvas, cx, cy, r, t float64) {
	cloudBody(c, cx, cy-r*0.28, r*1.26, 0.92)
	y0, span := cy+r*0.14, r*0.86
	const n = 5
	for i := 0; i < n; i++ {
		ph := math.Mod(t*0.80+float64(i)/float64(n), 1.0)
		yp := ph * ph
		x := cx - r*0.46 + float64(i)*r*0.23
		y := y0 + yp*span
		a := math.Sin(ph * math.Pi)
		l := r * (0.10 + 0.085*ph)
		c.Line(x, y, x-r*0.035, y+l, 1.7, WithA(wxRain, clampByte(a*238)))
	}
}

// ---- 雪：云 + 摆动飘落的雪粒 ----
func drawSnow(c *Canvas, cx, cy, r, t float64) {
	cloudBody(c, cx, cy-r*0.30, r*1.24, 0.88)
	const n = 5
	for i := 0; i < n; i++ {
		ph := math.Mod(t*0.34+float64(i)/float64(n), 1.0)
		// 比雨缓：轻的东西不该像石头一样砸下来
		yp := ph*ph*0.5 + ph*0.5
		x := cx - r*0.42 + float64(i)*r*0.21 +
			math.Sin(ph*7.2+float64(i)*1.9)*r*0.065
		y := cy + r*0.08 + yp*r*0.84
		a := math.Sin(ph * math.Pi)
		// 由远及近：越接近落点越大
		c.FillCircle(x, y, 1.35+1.15*ph, WithA(wxSnow, clampByte(a*242)))
	}
}

// ---- 雾：雾后淡日 + 三条各走各的雾带 ----
func drawFog(c *Canvas, cx, cy, r, t float64) {
	c.FillCircle(cx, cy-r*0.32, r*0.24, WithA(wxHaze, 92))
	c.FillCircle(cx, cy-r*0.32, r*0.17*easeIO(0.5+0.5*math.Sin(t*0.55)), WithA(wxSunHi, 108))

	ys := [3]float64{-r * 0.08, r * 0.18, r * 0.44}
	ws := [3]float64{0.92, 0.78, 0.60}
	for i := 0; i < 3; i++ {
		// 三条带的频率、幅度都不同，且两端渐隐（雾带没有"端点"）
		off := wobble(t+float64(i)*7.3, 0.24+float64(i)*0.08) * r * (0.10 + 0.05*float64(i))
		w := r * ws[i]
		fadeLine(c, cx-w/2+off, cx+w/2+off, cy+ys[i], 2.2, wxFog, 205)
	}
}

// ---- 雷雨：云 + 间歇闪电 + 雨 ----
//
// 旧版亮度是阶跃的（<0.09 就 1.0，然后 0.48、0.90、0.30）。阶跃在 30fps 下
// 就是"啪"地跳一下 —— 这正是要消掉的生硬。现在用两个高斯峰构成的平滑包络，
// 快起慢落，才是"闪"。闪电形状每轮换一条，避免看久了像贴图。
func drawThunder(c *Canvas, cx, cy, r, t float64) {
	cloudBody(c, cx, cy-r*0.30, r*1.26, 0.94)

	ph := math.Mod(t, 2.8)
	var br float64
	if ph < 0.34 {
		u := ph / 0.34
		pk := func(mu, sg, amp float64) float64 {
			d := (u - mu) / sg
			return amp * math.Exp(-d*d)
		}
		br = pk(0.16, 0.070, 1.00) + pk(0.55, 0.105, 0.72)
	}
	if br < 0.03 {
		br = 0.03
	}

	var pts [4][2]float64
	if int(t/2.8)%2 == 0 {
		pts = [4][2]float64{{0.06, -0.04}, {-0.16, 0.30}, {0.03, 0.30}, {-0.18, 0.70}}
	} else {
		pts = [4][2]float64{{-0.04, -0.06}, {0.14, 0.26}, {-0.05, 0.30}, {0.10, 0.72}}
	}
	// 先描一层更宽的暗边：暗态也能看清形状，亮起时又不会突然变胖
	for i := 0; i+1 < len(pts); i++ {
		c.Line(cx+r*pts[i][0], cy+r*pts[i][1], cx+r*pts[i+1][0], cy+r*pts[i+1][1],
			3.4, WithA(wxBolt, clampByte(br*62)))
	}
	for i := 0; i+1 < len(pts); i++ {
		c.Line(cx+r*pts[i][0], cy+r*pts[i][1], cx+r*pts[i+1][0], cy+r*pts[i+1][1],
			2.0, WithA(wxBolt, clampByte(br*255)))
	}

	const n = 4
	for i := 0; i < n; i++ {
		phr := math.Mod(t*0.92+float64(i)/float64(n), 1.0)
		yp := phr * phr
		x := cx - r*0.42 + float64(i)*r*0.28
		y := cy + r*0.12 + yp*r*0.74
		a := math.Sin(phr*math.Pi) * (0.55 + 0.32*br)
		l := r * (0.09 + 0.075*phr)
		c.Line(x, y, x-r*0.035, y+l, 1.7, WithA(wxRain, clampByte(a*255)))
	}
}
