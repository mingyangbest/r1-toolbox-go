package hardware

import (
	"os"
	"strconv"
	"strings"
)

type BrightnessController struct {
	path string
	min  int
	max  int
}

func NewBrightnessController(path string, min, max int) *BrightnessController {
	return &BrightnessController{path: path, min: min, max: max}
}

func (b *BrightnessController) Get() (int, error) {
	data, err := os.ReadFile(b.path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func (b *BrightnessController) Set(value int) error {
	if value < b.min {
		value = b.min
	}
	if value > b.max {
		value = b.max
	}
	return os.WriteFile(b.path, []byte(strconv.Itoa(value)), 0644)
}

// Max 返回硬件背光上限（读自 max_brightness），供接口层做入参校验，
// 避免越界值被原样写进配置文件持久化。
func (b *BrightnessController) Max() int { return b.max }
