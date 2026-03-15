package hardware

import (
	"fmt"
	"log"
	"os/exec"
	"time"
)

type RGBController struct {
	bus string
}

func NewRGBController() *RGBController {
	return &RGBController{bus: "0"}
}

func (r *RGBController) i2cSet(register, value int) error {
	cmd := exec.Command("i2cset", "-f", "-y", r.bus, "0x26", 
		fmt.Sprintf("0x%02x", register), fmt.Sprintf("0x%02x", value))
	return cmd.Run()
}

func (r *RGBController) SetMode(mode, color string) error {
	log.Printf("设置RGB灯: mode=%s color=%s", mode, color)
	
	// 重置RGB灯状态
	r.i2cSet(0x90, 0x0)
	r.i2cSet(0x92, 0)
	r.i2cSet(0x93, 0)
	r.i2cSet(0x94, 0)
	time.Sleep(200 * time.Millisecond)
	
	switch mode {
	case "off":
		log.Println("RGB灯关闭模式")
		return r.i2cSet(0x90, 0x0)
		
	case "rainbow":
		log.Println("RGB灯彩虹模式")
		return r.i2cSet(0x90, 0x2)
		
	case "breathing":
		rgb := getColorRGB(color)
		log.Printf("RGB灯呼吸模式: R=%d G=%d B=%d", rgb[0], rgb[1], rgb[2])
		r.i2cSet(0x92, rgb[0])
		r.i2cSet(0x93, rgb[1])
		r.i2cSet(0x94, rgb[2])
		time.Sleep(100 * time.Millisecond)
		return r.i2cSet(0x90, 0x3)
		
	case "solid":
		rgb := getColorRGB(color)
		log.Printf("RGB灯静态单色常亮模式: R=%d G=%d B=%d", rgb[0], rgb[1], rgb[2])
		r.i2cSet(0x92, rgb[0])
		r.i2cSet(0x93, rgb[1])
		r.i2cSet(0x94, rgb[2])
		time.Sleep(100 * time.Millisecond)
		return r.i2cSet(0x90, 0x1)
	}
	
	return nil
}

func getColorRGB(color string) [3]int {
	colors := map[string][3]int{
		"red":    {255, 0, 0},
		"orange": {255, 136, 0},
		"yellow": {255, 255, 0},
		"green":  {0, 255, 0},
		"cyan":   {0, 255, 255},
		"blue":   {0, 0, 255},
		"purple": {255, 0, 255},
		"white":  {255, 255, 255},
	}
	
	if rgb, ok := colors[color]; ok {
		return rgb
	}
	return [3]int{0, 255, 0} // 默认绿色
}
