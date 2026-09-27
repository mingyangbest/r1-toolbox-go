package ui

import (
	"fmt"
	"log"
	"time"

	"github.com/veandco/go-sdl2/sdl"

	"r1-toolbox/internal/hardware"
)

type TouchGesture int

const (
	GestureNone TouchGesture = iota
	GestureTap
	GestureSwipeUp
	GestureSwipeDown
	GestureSwipeLeft
	GestureSwipeRight
)

type TapEvent struct {
	X int32
	Y int32
}

type TouchHandler struct {
	startX, startY int32
	startTime      time.Time
	isDragging     bool
	touchScreen    *hardware.TouchScreen
	useI2C         bool
	gestureChan    chan TouchGesture
	tapChan        chan TapEvent
}

func NewTouchHandler() *TouchHandler {
	return &TouchHandler{}
}

// NewTouchHandlerWithI2C 创建使用I2C直接读取的触摸处理器
func NewTouchHandlerWithI2C() (*TouchHandler, error) {
	ts, err := hardware.NewTouchScreen()
	if err != nil {
		return nil, err
	}

	if err := ts.Open(); err != nil {
		return nil, err
	}

	handler := &TouchHandler{
		touchScreen: ts,
		useI2C:      true,
		gestureChan: make(chan TouchGesture, 10),
		tapChan:     make(chan TapEvent, 10),
	}

	go handler.readTouchLoop()

	return handler, nil
}

func (t *TouchHandler) readTouchLoop() {
	for t.useI2C && t.touchScreen != nil {
		point, err := t.touchScreen.ReadTouch()
		if err != nil || point == nil {
			continue
		}

		switch point.Event {
		case hardware.TouchPress:
			if !t.isDragging {
				t.startX = int32(point.X)
				t.startY = int32(point.Y)
				t.startTime = time.Now()
				t.isDragging = true
			}

		case hardware.TouchRelease:
			if t.isDragging {
				endX := int32(point.X)
				endY := int32(point.Y)
				gesture := t.detectGesture(endX, endY)
				if gesture == GestureTap {
					select {
					case t.tapChan <- TapEvent{X: endX, Y: endY}:
					default:
					}
				}
				if gesture != GestureNone {
					select {
					case t.gestureChan <- gesture:
					default:
					}
				}
			}
		}
	}
}

// Close 关闭触摸屏设备
func (t *TouchHandler) Close() error {
	if t.touchScreen != nil {
		return t.touchScreen.Close()
	}
	return nil
}

// ReadI2CTouch 从channel读取手势（非阻塞）
func (t *TouchHandler) ReadI2CTouch() TouchGesture {
	if !t.useI2C || t.touchScreen == nil {
		return GestureNone
	}
	select {
	case gesture := <-t.gestureChan:
		return gesture
	default:
		return GestureNone
	}
}

// ReadTap 读取点击事件坐标（非阻塞）
func (t *TouchHandler) ReadTap() *TapEvent {
	if !t.useI2C || t.touchScreen == nil {
		return nil
	}
	select {
	case tap := <-t.tapChan:
		return &tap
	default:
		return nil
	}
}

// HandleEvent 处理 SDL 事件，返回手势与（若为点击）坐标
func (t *TouchHandler) HandleEvent(event sdl.Event) (TouchGesture, *TapEvent) {
	switch e := event.(type) {
	case *sdl.MouseButtonEvent:
		if e.Type == sdl.MOUSEBUTTONDOWN {
			t.startX, t.startY = e.X, e.Y
			t.startTime = time.Now()
			t.isDragging = true
		} else if e.Type == sdl.MOUSEBUTTONUP && t.isDragging {
			g := t.detectGesture(e.X, e.Y)
			if g == GestureTap {
				return g, &TapEvent{X: e.X, Y: e.Y}
			}
			return g, nil
		}
	case *sdl.TouchFingerEvent:
		if e.Type == sdl.FINGERDOWN {
			t.startX = int32(e.X * 376)
			t.startY = int32(e.Y * 960)
			t.startTime = time.Now()
			t.isDragging = true
		} else if e.Type == sdl.FINGERUP && t.isDragging {
			ex := int32(e.X * 376)
			ey := int32(e.Y * 960)
			g := t.detectGesture(ex, ey)
			if g == GestureTap {
				return g, &TapEvent{X: ex, Y: ey}
			}
			return g, nil
		}
	}
	return GestureNone, nil
}

func (t *TouchHandler) detectGesture(endX, endY int32) TouchGesture {
	t.isDragging = false

	dx := endX - t.startX
	dy := endY - t.startY
	elapsed := time.Since(t.startTime).Milliseconds()

	// 点击判定放宽：位移 < 30px 且按住 < 700ms
	if abs(dx) < 30 && abs(dy) < 30 && elapsed < 700 {
		log.Printf("[触摸] 点击 (%d, %d)", endX, endY)
		return GestureTap
	}

	if abs(dx) > abs(dy) && abs(dx) > 50 {
		if dx > 0 {
			log.Printf("[触摸] 右滑")
			return GestureSwipeRight
		}
		log.Printf("[触摸] 左滑")
		return GestureSwipeLeft
	}

	if abs(dy) > 50 {
		if dy > 0 {
			log.Printf("[触摸] 下滑")
			return GestureSwipeDown
		}
		log.Printf("[触摸] 上滑")
		return GestureSwipeUp
	}

	fmt.Printf("[触摸] 未识别: dx=%d dy=%d elapsed=%dms\n", dx, dy, elapsed)
	return GestureNone
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
