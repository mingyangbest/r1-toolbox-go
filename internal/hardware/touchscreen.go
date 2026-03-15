package hardware

import (
	"fmt"
	"syscall"
	"time"
)

const (
	I2C_DEVICE     = "/dev/i2c-2"
	I2C_SLAVE_ADDR = 0x2C
	I2C_SLAVE      = 0x0703
	SCREEN_WIDTH   = 376
	SCREEN_HEIGHT  = 960
)

type TouchEvent int

const (
	TouchNone    TouchEvent = 0
	TouchPress   TouchEvent = 1
	TouchRelease TouchEvent = 2
)

type TouchPoint struct {
	Event     TouchEvent
	X         uint16
	Y         uint16
	Timestamp uint32
}

type TouchScreen struct {
	fd          int
	running     bool
	lastX       uint16
	lastY       uint16
	hasTouch    bool
	ringBuf     []byte
	ringLen     int
	zeroCount   int
}

func NewTouchScreen() (*TouchScreen, error) {
	return &TouchScreen{
		fd:      -1,
		running: false,
		ringBuf: make([]byte, 256),
	}, nil
}

func (ts *TouchScreen) Open() error {
	fd, err := syscall.Open(I2C_DEVICE, syscall.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open %s: %v", I2C_DEVICE, err)
	}
	ts.fd = fd

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(ts.fd), I2C_SLAVE, uintptr(I2C_SLAVE_ADDR))
	if errno != 0 {
		syscall.Close(ts.fd)
		return fmt.Errorf("failed to set I2C slave address: %v", errno)
	}

	fmt.Println("=== CST0836 Touch ===")
	fmt.Printf("Device: %s @ 0x%02x | Screen: %dx%d\n", I2C_DEVICE, I2C_SLAVE_ADDR, SCREEN_WIDTH, SCREEN_HEIGHT)

	// 写入初始化命令 (小端序 0x0001)
	initCmd := []byte{0x01, 0x00}
	_, err = syscall.Write(ts.fd, initCmd)
	if err != nil {
		syscall.Close(ts.fd)
		return fmt.Errorf("failed to initialize device: %v", err)
	}

	time.Sleep(1 * time.Millisecond)

	// 读取并丢弃HID描述符（持续读取直到全零）
	drainBuf := make([]byte, 64)
	for i := 0; i < 30; i++ {
		n, err := syscall.Read(ts.fd, drainBuf)
		if err != nil {
			break
		}
		allZero := true
		for j := 0; j < n; j++ {
			if drainBuf[j] != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	fmt.Println("Touch screen ready")
	ts.running = true
	return nil
}

func (ts *TouchScreen) Close() error {
	ts.running = false
	if ts.fd >= 0 {
		err := syscall.Close(ts.fd)
		ts.fd = -1
		return err
	}
	return nil
}

// ReadTouch 读取触摸事件（阻塞模式，使用环形缓冲区）
func (ts *TouchScreen) ReadTouch() (*TouchPoint, error) {
	if !ts.running || ts.fd < 0 {
		return nil, fmt.Errorf("touchscreen not opened")
	}

	readBuf := make([]byte, 64)
	n, err := syscall.Read(ts.fd, readBuf)
	if err != nil {
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}

	if n <= 0 {
		time.Sleep(20 * time.Millisecond)
		return nil, nil
	}

	// 检查是否全0
	allZero := true
	for i := 0; i < n; i++ {
		if readBuf[i] != 0 {
			allZero = false
			break
		}
	}

	if allZero {
		ts.zeroCount++
		// 全零帧：如果之前有触摸，生成释放事件
		if ts.hasTouch && ts.zeroCount > 2 {
			return ts.generateReleaseEvent(), nil
		}
		// 处理缓冲区中残留的数据
		if ts.ringLen > 0 {
			point := ts.scanBuffer()
			ts.ringLen = 0
			if point != nil {
				return point, nil
			}
		}
		time.Sleep(5 * time.Millisecond)
		return nil, nil
	}

	ts.zeroCount = 0

	// 追加到环形缓冲区
	if ts.ringLen+n > len(ts.ringBuf) {
		// 缓冲区满，尝试处理后清空
		point := ts.scanBuffer()
		ts.ringLen = 0
		if point != nil {
			// 保存新数据
			copy(ts.ringBuf, readBuf[:n])
			ts.ringLen = n
			return point, nil
		}
	}
	copy(ts.ringBuf[ts.ringLen:], readBuf[:n])
	ts.ringLen += n

	// 扫描缓冲区查找触摸数据
	point := ts.scanBuffer()
	return point, nil
}

// scanBuffer 扫描环形缓冲区查找触摸数据包
// 格式: 20 00 [event] 03 [x_lo] [x_hi] [y_lo] [y_hi]
func (ts *TouchScreen) scanBuffer() *TouchPoint {
	buf := ts.ringBuf[:ts.ringLen]
	var lastPoint *TouchPoint

	for i := 0; i+7 < len(buf); i++ {
		if buf[i] != 0x20 || buf[i+1] != 0x00 || buf[i+3] != 0x03 {
			continue
		}

		eventType := buf[i+2] & 0x0F
		x := uint16(buf[i+4]) | (uint16(buf[i+5]) << 8)
		y := uint16(buf[i+6]) | (uint16(buf[i+7]) << 8)

		if x > SCREEN_WIDTH || y > SCREEN_HEIGHT {
			continue
		}
		if x == 0 && y == 0 {
			continue
		}
		if x == 0 && y <= 99 {
			continue
		}

		if eventType == 1 {
			ts.hasTouch = true
			ts.lastX = x
			ts.lastY = y
			lastPoint = &TouchPoint{
				Event:     TouchPress,
				X:         x,
				Y:         y,
				Timestamp: uint32(time.Now().UnixMilli()),
			}
		} else if eventType == 0 && ts.hasTouch {
			lastPoint = ts.generateReleaseEvent()
		}

		// 消耗已处理的数据
		consumed := i + 8
		copy(ts.ringBuf, ts.ringBuf[consumed:ts.ringLen])
		ts.ringLen -= consumed
	}

	return lastPoint
}

func (ts *TouchScreen) generateReleaseEvent() *TouchPoint {
	ts.hasTouch = false
	oldX, oldY := ts.lastX, ts.lastY
	ts.lastX = 0
	ts.lastY = 0

	return &TouchPoint{
		Event:     TouchRelease,
		X:         oldX,
		Y:         oldY,
		Timestamp: uint32(time.Now().UnixMilli()),
	}
}

// StartMonitor 启动触摸监控
func (ts *TouchScreen) StartMonitor(callback func(*TouchPoint)) error {
	if err := ts.Open(); err != nil {
		return err
	}

	go func() {
		for ts.running {
			point, err := ts.ReadTouch()
			if err != nil {
				continue
			}
			if point != nil && callback != nil {
				callback(point)
			}
		}
	}()

	return nil
}
