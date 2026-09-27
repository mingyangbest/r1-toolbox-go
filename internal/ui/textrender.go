package ui

import (
	"sync"

	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
)

// ============================================================
//  字体管理 + 文字纹理缓存
//
//  双字体族：
//    - CJK   ：文泉驿微米黑（中文，字形完整）
//    - LATIN ：Cantarell Thin / Light（数字与拉丁，纤细几何感）
//  两族分开渲染、分开缓存，由调用方决定用哪一族 —— 排版完全可控，
//  避免依赖自动字体回退带来的基线错位。
// ============================================================

// 中文字体候选
var cjkSearchPaths = []string{
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
	"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
	"/usr/share/fonts/wqy-microhei/wqy-microhei.ttc",
	"/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

// 拉丁字体候选：大字号用 Thin（纤细），小字号用 Light（可读）
var latinThinPaths = []string{
	"/usr/share/fonts/opentype/cantarell/Cantarell-Thin.otf",
	"/usr/share/fonts/opentype/cantarell/Cantarell-Light.otf",
	"/usr/share/fonts/opentype/urw-base35/NimbusSans-Regular.otf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

var latinLightPaths = []string{
	"/usr/share/fonts/opentype/cantarell/Cantarell-Light.otf",
	"/usr/share/fonts/opentype/cantarell/Cantarell-Regular.otf",
	"/usr/share/fonts/opentype/urw-base35/NimbusSans-Regular.otf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
}

func probeFont(paths []string) string {
	for _, p := range paths {
		if f, err := ttf.OpenFont(p, 16); err == nil {
			f.Close()
			return p
		}
	}
	return ""
}

// 大字号阈值：>= 此值用 Thin 字重
const latinThinMinSize = 40

type FontSet struct {
	cjkPath   string
	thinPath  string
	lightPath string

	cjk   map[int]*ttf.Font
	thin  map[int]*ttf.Font
	light map[int]*ttf.Font
}

func NewFontSet() *FontSet {
	fs := &FontSet{
		cjk:   map[int]*ttf.Font{},
		thin:  map[int]*ttf.Font{},
		light: map[int]*ttf.Font{},
	}
	fs.cjkPath = probeFont(cjkSearchPaths)
	fs.thinPath = probeFont(latinThinPaths)
	fs.lightPath = probeFont(latinLightPaths)
	if fs.lightPath == "" {
		fs.lightPath = fs.cjkPath
	}
	if fs.thinPath == "" {
		fs.thinPath = fs.lightPath
	}
	return fs
}

// Path 返回中文字体路径（兼容旧接口）
func (f *FontSet) Path() string { return f.cjkPath }

// LatinPath 返回拉丁字体路径
func (f *FontSet) LatinPath() string { return f.lightPath }

// fontFor 按字号选择拉丁字重
func (f *FontSet) latinPathFor(size int) string {
	if size >= latinThinMinSize {
		return f.thinPath
	}
	return f.lightPath
}

func openCached(cache map[int]*ttf.Font, path string, size int) *ttf.Font {
	if size < 6 {
		size = 6
	}
	if fo, ok := cache[size]; ok {
		return fo
	}
	if path == "" {
		return nil
	}
	fo, err := ttf.OpenFont(path, size)
	if err != nil {
		return nil
	}
	cache[size] = fo
	return fo
}

// Get 取中文字体
func (f *FontSet) Get(size int) *ttf.Font {
	return openCached(f.cjk, f.cjkPath, size)
}

// GetLatin 取拉丁字体（数字/英文）
func (f *FontSet) GetLatin(size int) *ttf.Font {
	return openCached(f.light, f.latinPathFor(size), size)
}

func (f *FontSet) Close() {
	for _, m := range []map[int]*ttf.Font{f.cjk, f.thin, f.light} {
		for _, fo := range m {
			if fo != nil {
				fo.Close()
			}
		}
	}
	f.cjk = map[int]*ttf.Font{}
	f.thin = map[int]*ttf.Font{}
	f.light = map[int]*ttf.Font{}
}

// ------------------------------------------------------------
//  文字位图（RGBA）
// ------------------------------------------------------------

type glyphImg struct {
	w, h int
	pix  []byte // RGBA
	// 可见字形（非透明像素）的边界；ix1 < ix0 表示空
	ix0, iy0, ix1, iy1 int
}

// inkW 可见字形宽度
func (im *glyphImg) inkW() int {
	if im == nil || im.ix1 < im.ix0 {
		return 0
	}
	return im.ix1 - im.ix0 + 1
}

// inkH 可见字形高度
func (im *glyphImg) inkH() int {
	if im == nil || im.iy1 < im.iy0 {
		return 0
	}
	return im.iy1 - im.iy0 + 1
}

type glyphCache struct {
	mu sync.Mutex
	m  map[string]*glyphImg
}

var gcache = &glyphCache{m: map[string]*glyphImg{}}

const glyphCacheMax = 768

func (g *glyphCache) lookup(key string) (*glyphImg, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	im, ok := g.m[key]
	return im, ok
}

func (g *glyphCache) store(key string, im *glyphImg) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.m) >= glyphCacheMax {
		g.m = map[string]*glyphImg{}
	}
	g.m[key] = im
}

func colorKey(col RGBA) string {
	return strconvItoa(int(col.R)) + "," + strconvItoa(int(col.G)) + "," + strconvItoa(int(col.B))
}

// glyph 中文/默认字体
func glyph(fs *FontSet, size int, s string, col RGBA) *glyphImg {
	key := "c|" + strconvItoa(size) + "|" + colorKey(col) + "|" + s
	if im, ok := gcache.lookup(key); ok {
		return im
	}
	im := renderGlyph(fs.Get(size), s, col)
	gcache.store(key, im)
	return im
}

// glyphLatin 拉丁字体（数字/英文）
func glyphLatin(fs *FontSet, size int, s string, col RGBA) *glyphImg {
	key := "l|" + strconvItoa(size) + "|" + colorKey(col) + "|" + s
	if im, ok := gcache.lookup(key); ok {
		return im
	}
	im := renderGlyph(fs.GetLatin(size), s, col)
	gcache.store(key, im)
	return im
}

// maskVal 从打包像素值中按掩码取出 8 位通道值
func maskVal(mask, v uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := uint(0)
	m := mask
	for m&1 == 0 && shift < 32 {
		m >>= 1
		shift++
	}
	return uint8((v & mask) >> shift)
}

func renderGlyph(font *ttf.Font, s string, col RGBA) *glyphImg {
	empty := &glyphImg{}
	if font == nil || s == "" {
		return empty
	}
	surf, err := font.RenderUTF8Blended(s, sdl.Color{R: col.R, G: col.G, B: col.B, A: 255})
	if err != nil || surf == nil {
		return empty
	}
	defer surf.Free()

	w, h := int(surf.W), int(surf.H)
	if w <= 0 || h <= 0 {
		return empty
	}
	pitch := int(surf.Pitch)
	px := surf.Pixels()
	fm := surf.Format
	out := make([]byte, w*h*4)
	ix0, iy0, ix1, iy1 := w, h, -1, -1
	for y := 0; y < h; y++ {
		rowIn := y * pitch
		rowOut := y * w * 4
		for x := 0; x < w; x++ {
			i := rowIn + x*4
			if i+3 >= len(px) {
				continue
			}
			v := uint32(px[i]) | uint32(px[i+1])<<8 | uint32(px[i+2])<<16 | uint32(px[i+3])<<24
			var a uint8
			if fm.Amask != 0 {
				a = maskVal(fm.Amask, v)
			} else {
				a = 255
			}
			// 以较低阈值判定"有墨"，避免细字重被误判为空
			if a < 12 {
				continue
			}
			r := maskVal(fm.Rmask, v)
			g := maskVal(fm.Gmask, v)
			b := maskVal(fm.Bmask, v)
			j := rowOut + x*4
			out[j], out[j+1], out[j+2], out[j+3] = r, g, b, a
			if x < ix0 {
				ix0 = x
			}
			if x > ix1 {
				ix1 = x
			}
			if y < iy0 {
				iy0 = y
			}
			if y > iy1 {
				iy1 = y
			}
		}
	}
	return &glyphImg{w: w, h: h, pix: out, ix0: ix0, iy0: iy0, ix1: ix1, iy1: iy1}
}

// ------------------------------------------------------------
//  绘制接口
// ------------------------------------------------------------

func blitGlyph(c *Canvas, im *glyphImg, x, y int) {
	if im == nil || im.w == 0 {
		return
	}
	// 裁剪粗筛：字形位图整块落在裁剪区外就别逐像素走了。
	// 页面滑动时有一半的字都画在窗外，这一句省下的是滑动的帧时间。
	if !c.clipVisible(x, y, im.w, im.h) {
		return
	}
	gx0, gx1 := c.clampX(x, x+im.w-1)
	iy0, _ := c.clampY(y, y+im.h-1)
	for yy := iy0 - y; yy < im.h; yy++ {
		row := yy * im.w * 4
		for xx := gx0 - x; xx <= gx1-x; xx++ {
			j := row + xx*4
			a := im.pix[j+3]
			if a == 0 {
				continue
			}
			c.Blend(x+xx, y+yy, RGBA{im.pix[j], im.pix[j+1], im.pix[j+2], 255}, float64(a)/255)
		}
	}
}

// DrawText 在 (x,y) 以左上角为锚点绘制文字（中文字体），返回宽度
func DrawText(c *Canvas, fs *FontSet, s string, x, y, size int, col RGBA) int {
	im := glyph(fs, size, s, col)
	blitGlyph(c, im, x, y)
	return im.w
}

// DrawTextL 同上，但使用拉丁字体（数字 / 英文）
func DrawTextL(c *Canvas, fs *FontSet, s string, x, y, size int, col RGBA) int {
	im := glyphLatin(fs, size, s, col)
	blitGlyph(c, im, x, y)
	return im.w
}

// DrawTextC 水平居中（按可见字形居中，中文字体）
func DrawTextC(c *Canvas, fs *FontSet, s string, cx, y, size int, col RGBA) int {
	im := glyph(fs, size, s, col)
	if im.inkW() == 0 {
		return 0
	}
	blitGlyph(c, im, cx-(im.ix0+im.ix1)/2, y)
	return im.inkW()
}

// DrawTextLC 水平居中（拉丁字体）
func DrawTextLC(c *Canvas, fs *FontSet, s string, cx, y, size int, col RGBA) int {
	im := glyphLatin(fs, size, s, col)
	if im.inkW() == 0 {
		return 0
	}
	blitGlyph(c, im, cx-(im.ix0+im.ix1)/2, y)
	return im.inkW()
}

// DrawTextR 右对齐（rx 为右边界，中文字体）
func DrawTextR(c *Canvas, fs *FontSet, s string, rx, y, size int, col RGBA) int {
	im := glyph(fs, size, s, col)
	if im.inkW() == 0 {
		return 0
	}
	blitGlyph(c, im, rx-im.ix1-1, y)
	return im.inkW()
}

// DrawTextLR 右对齐（拉丁字体）
func DrawTextLR(c *Canvas, fs *FontSet, s string, rx, y, size int, col RGBA) int {
	im := glyphLatin(fs, size, s, col)
	if im.inkW() == 0 {
		return 0
	}
	blitGlyph(c, im, rx-im.ix1-1, y)
	return im.inkW()
}

// TextWidth 中文/默认字体宽度
func TextWidth(fs *FontSet, s string, size int) int {
	return glyph(fs, size, s, RGBA{255, 255, 255, 255}).w
}

// TextWidthL 拉丁字体宽度
func TextWidthL(fs *FontSet, s string, size int) int {
	return glyphLatin(fs, size, s, RGBA{255, 255, 255, 255}).w
}

// TextHeight 中文/默认字体高度
func TextHeight(fs *FontSet, s string, size int) int {
	return glyph(fs, size, s, RGBA{255, 255, 255, 255}).h
}

// TextHeightL 拉丁字体高度
func TextHeightL(fs *FontSet, s string, size int) int {
	return glyphLatin(fs, size, s, RGBA{255, 255, 255, 255}).h
}

// InkWidth 可见字形宽度（中文字体）—— 用于精确居中 / 对齐 / 装饰线
func InkWidth(fs *FontSet, s string, size int) int {
	return glyph(fs, size, s, RGBA{255, 255, 255, 255}).inkW()
}

// InkWidthL 可见字形宽度（拉丁字体）
func InkWidthL(fs *FontSet, s string, size int) int {
	return glyphLatin(fs, size, s, RGBA{255, 255, 255, 255}).inkW()
}

// 轻量 itoa，避免引入 fmt 到热路径
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
