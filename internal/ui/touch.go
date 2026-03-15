package ui

import (
	"fmt"
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
	
	// 启动后台goroutine读取触摸事件
	go handler.readTouchLoop()
	
	return handler, nil
}

// readTouchLoop 在后台goroutine中读取触摸事件
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
				fmt.Printf("[Gesture] 开始拖动: 起点(%d, %d)\n", t.startX, t.startY)
			}
			
		case hardware.TouchRelease:
			if t.isDragging {
				endX := int32(point.X)
				endY := int32(point.Y)
				fmt.Printf("[Gesture] 结束拖动: 终点(%d, %d)\n", endX, endY)
				gesture := t.detectGesture(endX, endY)
				if gesture == GestureTap {
					// 点击事件，发送坐标
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

func (t *TouchHandler) HandleEvent(event sdl.Event) TouchGesture {
	switch e := event.(type) {
	case *sdl.MouseButtonEvent:
		if e.Type == sdl.MOUSEBUTTONDOWN {
			t.startX, t.startY = e.X, e.Y
			t.startTime = time.Now()
			t.isDragging = true
			fmt.Printf("[Gesture SDL] 鼠标按下: (%d, %d)\n", t.startX, t.startY)
		} else if e.Type == sdl.MOUSEBUTTONUP && t.isDragging {
			fmt.Printf("[Gesture SDL] 鼠标释放: (%d, %d)\n", e.X, e.Y)
			return t.detectGesture(e.X, e.Y)
		}
	case *sdl.TouchFingerEvent:
		if e.Type == sdl.FINGERDOWN {
			t.startX, t.startY = int32(e.X*376), int32(e.Y*960)
			t.startTime = time.Now()
			t.isDragging = true
			fmt.Printf("[Gesture SDL] 手指按下: (%d, %d)\n", t.startX, t.startY)
		} else if e.Type == sdl.FINGERUP && t.isDragging {
			endX := int32(e.X * 376)
			endY := int32(e.Y * 960)
			fmt.Printf("[Gesture SDL] 手指释放: (%d, %d)\n", endX, endY)
			return t.detectGesture(endX, endY)
		}
	}
	return GestureNone
}

func (t *TouchHandler) detectGesture(endX, endY int32) TouchGesture {
	t.isDragging = false
	
	dx := endX - t.startX
	dy := endY - t.startY
	elapsed := time.Since(t.startTime).Milliseconds()
	
	fmt.Printf("[Gesture] 检测手势: dx=%d dy=%d elapsed=%dms\n", dx, dy, elapsed)
	
	if abs(dx) < 20 && abs(dy) < 20 && elapsed < 300 {
		fmt.Printf("[Gesture] ✓ 识别为: 点击\n")
		return GestureTap
	}
	
	if abs(dx) > abs(dy) && abs(dx) > 50 {
		if dx > 0 {
			fmt.Printf("[Gesture] ✓ 识别为: 右滑\n")
			return GestureSwipeRight
		}
		fmt.Printf("[Gesture] ✓ 识别为: 左滑\n")
		return GestureSwipeLeft
	}
	
	if abs(dy) > 50 {
		if dy > 0 {
			fmt.Printf("[Gesture] ✓ 识别为: 下滑\n")
			return GestureSwipeDown
		}
		fmt.Printf("[Gesture] ✓ 识别为: 上滑\n")
		return GestureSwipeUp
	}
	
	fmt.Printf("[Gesture] 无法识别手势\n")
	return GestureNone
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
