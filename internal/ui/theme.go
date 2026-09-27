package ui

// ============================================================
//  主题
//
//  设计原则（去"AI 味"）：
//   1. 全屏只允许出现一种强调色，其余全是灰阶
//   2. 状态色（琥珀/砖红）只在越过阈值时才出现，正常状态不用绿色
//   3. 所有颜色低饱和 —— 不做霓虹、不做发光
//
//  第二轮修正（2026-09-27）：上一版把卡片压到几乎看不见（alpha 8），
//  结果是信息像浮在黑色虚空里，观感"太素"。现在把卡片重新做成
//  「看得见的面」（alpha 15 / 描边 26），并建立三级文字层次
//  （Title 标题 / Text 数值 / Dim 标签），靠结构与内容拉开层次，
//  而不是回头去加渐变、发光、多色那些"AI 味"元素。
//
//  第三轮修正（2026-09-27）：洋哥仍然觉得"每页都太素"。
//  这次的做法不是把强调色调饱和，而是引入 **语义调色板 Pal** ——
//  让每一种颜色固定对应一个部件（橙=CPU、青=内存、紫=存储、
//  蓝=上行、绿=下行）。颜色一旦承担含义，密度再高也不会显得乱，
//  这是"有色彩"与"有 AI 味"的分界线：
//    · 装饰性用色（无含义地撒颜色）→ AI 味
//    · 语义性用色（颜色 = 数据身份）→ 专业仪表
//  主题本身仍然只管背景与中性灰阶，与 Pal 完全解耦。
// ============================================================

type Theme struct {
	Name string

	BgTop, BgBot RGBA // 背景垂直渐变（极微）

	Card       RGBA // 卡片底色
	CardBorder RGBA // 卡片描边
	Hairline   RGBA // 分隔线

	Title RGBA // 卡片标题
	Text  RGBA // 主文字 / 数值
	Sub   RGBA // 次级文字
	Dim   RGBA // 弱化文字（标签）
	Faint RGBA // 更弱

	Accent  RGBA // 主强调色（全局唯一彩色）
	Accent2 RGBA // 次级强调（灰阶）
	Track   RGBA // 进度轨道

	Warn   RGBA
	Danger RGBA
}

// Status 旧接口：没有语义色时的状态色（常态用主题强调色）。
// 新代码请用 On(base, pct) —— 常态保留部件本身的语义色。
func (t *Theme) Status(pct float64) RGBA {
	return t.OnAt(t.Accent, pct, 75, 90)
}

// Temp 旧接口：CPU 温度色（阈值 72 / 82 °C）。新代码用 OnTemp(base, c)。
func (t *Theme) Temp(c float64) RGBA {
	return t.OnAt(t.Accent, c, 72, 82)
}

// On 在「语义色」之上叠加「越阈值告警」。
//
// 这是本轮颜色体系的关键：颜色要同时表达两件事 ——
//   ① 身份：这是 CPU（橙）、这是内存（青）、这是存储（紫）
//   ② 状态：它是不是快满了
// 所以常态保留语义色，只有越过 75% / 90% 才跳琥珀 / 砖红。
// 于是「CPU 65%」是橙、「CPU 80%」是金黄、「CPU 95%」是砖红，
// 一眼就能从颜色读出是哪一项、以及有多紧张。
func (t *Theme) On(base RGBA, pct float64) RGBA {
	return t.OnAt(base, pct, 75, 90)
}

// OnTemp 温度版（阈值 72 / 82 °C）
func (t *Theme) OnTemp(base RGBA, c float64) RGBA {
	return t.OnAt(base, c, 72, 82)
}

// OnLoad 负载三态色（2026-09-27 洋哥指定，适用于 CPU / 内存 / 硬盘容量）：
//
//	≤50% 绿（宽裕） → >50% 琥珀黄（偏忙） → ≥90% 砖红（告急）
//
// 这三类负载不再走「语义色 + 75/90」的老规则 —— 常态一律绿色，
// 颜色本身就是负载读数：绿=宽裕、黄=偏忙、红=告急。
// 绿取调色板里网络下行的青草绿（110,202,126）。
func (t *Theme) OnLoad(pct float64) RGBA {
	switch {
	case pct >= 90:
		return t.Danger
	case pct > 50:
		return t.Warn
	default:
		return P.NetDown
	}
}

// OnAt 通用「语义色 + 告警」叠加
func (t *Theme) OnAt(base RGBA, v, warnAt, dangerAt float64) RGBA {
	switch {
	case v >= dangerAt:
		return t.Danger
	case v >= warnAt:
		return t.Warn
	default:
		return base
	}
}

// ------------------------------------------------------------

// ink（墨）冷灰蓝 —— 默认
var themeInk = &Theme{
	Name: "ink",

	BgTop: C(19, 23, 27),
	BgBot: C(7, 8, 10),

	Card:       RGBA{255, 255, 255, 15},
	CardBorder: RGBA{255, 255, 255, 26},
	Hairline:   RGBA{255, 255, 255, 24},

	Title: C(170, 179, 190),
	Text:  C(238, 242, 246),
	Sub:   C(152, 161, 171),
	Dim:   C(99, 107, 117),
	Faint: RGBA{255, 255, 255, 22},

	Accent:  C(168, 188, 203),
	Accent2: C(122, 134, 145),
	Track:   RGBA{255, 255, 255, 32},

	Warn:   C(233, 193, 98),
	Danger: C(219, 98, 86),
}

// mono（素）中性灰
var themeMono = &Theme{
	Name: "mono",

	BgTop: C(21, 22, 24),
	BgBot: C(8, 8, 9),

	Card:       RGBA{255, 255, 255, 16},
	CardBorder: RGBA{255, 255, 255, 28},
	Hairline:   RGBA{255, 255, 255, 25},

	Title: C(174, 176, 180),
	Text:  C(238, 239, 241),
	Sub:   C(154, 157, 161),
	Dim:   C(100, 102, 105),
	Faint: RGBA{255, 255, 255, 22},

	Accent:  C(211, 215, 220),
	Accent2: C(139, 143, 148),
	Track:   RGBA{255, 255, 255, 32},

	Warn:   C(234, 196, 102),
	Danger: C(221, 102, 90),
}

// sand（沙）暖褐
var themeSand = &Theme{
	Name: "sand",

	BgTop: C(24, 21, 16),
	BgBot: C(10, 8, 6),

	Card:       RGBA{255, 245, 230, 16},
	CardBorder: RGBA{255, 245, 230, 28},
	Hairline:   RGBA{255, 245, 230, 25},

	Title: C(178, 167, 154),
	Text:  C(242, 236, 228),
	Sub:   C(158, 148, 138),
	Dim:   C(105, 98, 89),
	Faint: RGBA{255, 240, 225, 22},

	Accent:  C(198, 176, 140),
	Accent2: C(137, 125, 108),
	Track:   RGBA{255, 245, 230, 32},

	Warn:   C(238, 192, 88),
	Danger: C(222, 94, 80),
}

// jade（青）深青
var themeJade = &Theme{
	Name: "jade",

	BgTop: C(14, 22, 21),
	BgBot: C(5, 8, 8),

	Card:       RGBA{235, 255, 250, 16},
	CardBorder: RGBA{235, 255, 250, 28},
	Hairline:   RGBA{235, 255, 250, 25},

	Title: C(158, 174, 169),
	Text:  C(233, 241, 238),
	Sub:   C(147, 162, 157),
	Dim:   C(94, 107, 104),
	Faint: RGBA{235, 255, 250, 22},

	Accent:  C(156, 192, 180),
	Accent2: C(112, 130, 126),
	Track:   RGBA{235, 255, 250, 32},

	Warn:   C(230, 192, 96),
	Danger: C(214, 96, 86),
}

// ============================================================
//  语义调色板（Pal）—— 颜色 = 含义
//
//  与主题**解耦**：主题（墨/素/沙/青）只决定背景与中性灰阶，
//  而「哪个部件 = 哪个色相」在任何主题下都固定不变：
//
//      暖橙 = CPU      青绿 = 内存      紫色 = 存储
//      蓝色 = 下行     玫紫 = 上行      暖黄 = 风扇
//
//  色相彼此间隔 ≥ 45°，保证在深色底上能立刻分辨；
//  饱和度控制在中等（不是纯色），亮而不刺眼。
// ============================================================

type Pal struct {
	Cpu     RGBA // 暖橙 —— CPU 负载 / 核心 / CPU 曲线
	Mem     RGBA // 青绿 —— 内存 / 交换
	DiskSSD RGBA // 紫   —— 固态存储卷
	DiskHDD RGBA // 蓝   —— 机械存储卷
	NetDown RGBA // 绿   —— 网络下行（↓ 下载）
	NetUp   RGBA // 蓝   —— 网络上行（↑ 上传）
	Fan     RGBA // 暖黄 —— 风扇转速
	Sys     RGBA // 淡蓝 —— 系统信息 / 主机标识
}

// 网络配色（2026-09-27 洋哥指定）：↑ 上传 = 蓝、↓ 下载 = 绿。
//
// 上一版是「下载蓝 / 上传玫」，与洋哥给的参考卡片（↑蓝 ↓绿）不一致。
// 换过来之后，全站的网络语义统一成这一套：首页网络卡、网络页的
// 双曲线与累计流量、底部页签，都是「蓝 = 上行、绿 = 下行」。
// 绿取偏黄的青草绿（色相 ~130°），与内存的青绿（~172°）保持可分辨距离。
var P = &Pal{
	Cpu:     C(229, 154, 90),
	Mem:     C(87, 194, 180),
	DiskSSD: C(162, 140, 226),
	DiskHDD: C(102, 168, 232),
	NetDown: C(110, 202, 126),
	NetUp:   C(102, 168, 232),
	Fan:     C(217, 182, 94),
	Sys:     C(120, 176, 224),
}

// DiskColor 按介质类型取存储卷的语义色
func (p *Pal) DiskColor(deviceType string) RGBA {
	switch deviceType {
	case "HDD":
		return p.DiskHDD
	case "eMMC":
		return p.Mem
	default: // NVMe SSD / SSD
		return p.DiskSSD
	}
}

// ThemeByName 按名称取主题，未知则返回默认
func ThemeByName(name string) *Theme {
	switch name {
	case "mono":
		return themeMono
	case "sand":
		return themeSand
	case "jade":
		return themeJade
	default:
		return themeInk
	}
}

// ThemeNames 返回所有主题名（供循环切换）
func ThemeNames() []string {
	return []string{"ink", "mono", "sand", "jade"}
}

// ThemeLabel 主题的中文名
func ThemeLabel(name string) string {
	switch name {
	case "mono":
		return "素"
	case "sand":
		return "沙"
	case "jade":
		return "青"
	default:
		return "墨"
	}
}
