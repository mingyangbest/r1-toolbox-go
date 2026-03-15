package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	I2C_DEVICE     = "/dev/i2c-2"
	I2C_SLAVE      = 0x0703
	I2C_SLAVE_ADDR = 0x2C
	SCREEN_WIDTH   = 376
	SCREEN_HEIGHT  = 960
)

func main() {
	fmt.Println("=== CST0836 触摸屏独立测试 v3 ===")

	// 1. 打开I2C设备
	fd, err := syscall.Open(I2C_DEVICE, syscall.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开设备失败: %v\n", err)
		os.Exit(1)
	}
	defer syscall.Close(fd)
	fmt.Printf("✓ 打开设备 %s fd=%d\n", I2C_DEVICE, fd)

	// 2. 设置I2C从设备地址
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), I2C_SLAVE, uintptr(I2C_SLAVE_ADDR))
	if errno != 0 {
		fmt.Fprintf(os.Stderr, "设置I2C地址失败: %v\n", errno)
		os.Exit(1)
	}
	fmt.Printf("✓ 设置I2C地址 0x%02x\n", I2C_SLAVE_ADDR)

	// 3. 写入初始化命令
	initCmd := []byte{0x01, 0x00}
	n, err := syscall.Write(fd, initCmd)
	fmt.Printf("✓ 写入初始化命令: %02x %02x, 写入%d字节, err=%v\n", initCmd[0], initCmd[1], n, err)

	// 4. 等待1ms
	time.Sleep(1 * time.Millisecond)

	// 5. 读取HID描述符（持续读取直到全部读完）
	fmt.Println("\n--- 读取HID描述符 ---")
	drainBuf := make([]byte, 64)
	totalHidBytes := 0
	for i := 0; i < 30; i++ {
		n, err = syscall.Read(fd, drainBuf)
		if err != nil {
			break
		}
		totalHidBytes += n

		allZero := true
		for j := 0; j < n; j++ {
			if drainBuf[j] != 0 {
				allZero = false
				break
			}
		}

		if allZero {
			fmt.Printf("  描述符读取完毕，共读取 %d 字节 (%d次)\n", totalHidBytes, i+1)
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	fmt.Println("\n--- 等待触摸数据 ---")
	fmt.Println("请触摸屏幕 (Ctrl+C 退出)\n")

	// 信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	running := true
	go func() {
		<-sigChan
		running = false
	}()

	// 6. 使用环形缓冲区读取触摸数据
	ringBuf := make([]byte, 256)
	ringLen := 0
	readBuf := make([]byte, 64)
	frameCount := 0
	zeroCount := 0

	for running {
		n, err := syscall.Read(fd, readBuf)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		if n <= 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		frameCount++

		// 检查是否全0
		allZero := true
		for i := 0; i < n; i++ {
			if readBuf[i] != 0 {
				allZero = false
				break
			}
		}

		if allZero {
			zeroCount++
			// 如果之前有数据，全零表示数据结束
			if ringLen > 0 {
				fmt.Printf("[帧%d] 收到全零，处理缓冲区 (%d字节)\n", frameCount, ringLen)
				processBuffer(ringBuf[:ringLen])
				ringLen = 0
			}
			if zeroCount%200 == 0 {
				fmt.Printf("[帧%d] 等待触摸中...\n", frameCount)
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}

		zeroCount = 0

		// 追加到环形缓冲区
		if ringLen+n > len(ringBuf) {
			// 缓冲区满了，先处理
			fmt.Printf("[帧%d] 缓冲区满，处理 (%d字节)\n", frameCount, ringLen)
			processBuffer(ringBuf[:ringLen])
			ringLen = 0
		}
		copy(ringBuf[ringLen:], readBuf[:n])
		ringLen += n

		// 尝试在当前缓冲区中查找并处理触摸数据
		consumed := scanAndProcess(ringBuf[:ringLen], frameCount)
		if consumed > 0 {
			// 移除已处理的数据
			copy(ringBuf, ringBuf[consumed:ringLen])
			ringLen -= consumed
		}
	}

	fmt.Println("\n退出")
}

// scanAndProcess 扫描缓冲区查找触摸数据包，返回消耗的字节数
func scanAndProcess(buf []byte, frame int) int {
	consumed := 0
	for i := 0; i+7 < len(buf); i++ {
		// 查找触摸数据包标记: 20 00 XX 03
		// 格式: 20 00 [event] 03 [x_lo] [x_hi] [y_lo] [y_hi]
		if buf[i] == 0x20 && buf[i+1] == 0x00 && buf[i+3] == 0x03 {
			eventType := buf[i+2] & 0x0F
			xLo := buf[i+4]
			xHi := buf[i+5]
			yLo := buf[i+6]
			yHi := buf[i+7]
			x := uint16(xLo) | (uint16(xHi) << 8)
			y := uint16(yLo) | (uint16(yHi) << 8)

			if x <= SCREEN_WIDTH && y <= SCREEN_HEIGHT {
				ts := time.Now().UnixMilli()
				switch eventType {
				case 0:
					fmt.Printf("[帧%d] ★ 释放 X=%d Y=%d ts=%d\n", frame, x, y, ts)
				case 1:
					fmt.Printf("[帧%d] ★ 按下 X=%d Y=%d ts=%d\n", frame, x, y, ts)
				default:
					fmt.Printf("[帧%d] ★ 事件%d X=%d Y=%d ts=%d\n", frame, eventType, x, y, ts)
				}
				consumed = i + 8
			}
		}
	}
	return consumed
}

// processBuffer 处理整个缓冲区
func processBuffer(buf []byte) {
	fmt.Printf("  缓冲区 %d字节: ", len(buf))
	for i := 0; i < len(buf) && i < 32; i++ {
		fmt.Printf("%02x ", buf[i])
	}
	fmt.Println()
	scanAndProcess(buf, 0)
}
