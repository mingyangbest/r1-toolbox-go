# CST0836 触摸屏驱动


## 硬件信息

- **设备**: CST0836 触摸屏控制器
- **接口**: I2C (通过 `/dev/i2c-2`)
- **I2C地址**: 0x2C (44)
- **屏幕分辨率**: 376x960

## 功能特性

- 直接通过I2C读取触摸数据
- 支持触摸按下和释放事件
- 实时坐标报告
- 自动处理触摸释放检测

## 使用方法

### 1. 独立测试程序

编译测试程序：
```bash
make test-touch
```

运行测试（需要root权限访问I2C设备）：
```bash
sudo ./build/test-touch
```

输出格式：
```
时间戳 X坐标 Y坐标    # 按下事件
时间戳 -1 -1          # 释放事件
```

### 2. 集成到主程序

在主程序中使用I2C触摸屏：

```go
import "r1-toolbox/internal/hardware"

// 创建触摸屏控制器
ts, err := hardware.NewTouchScreen()
if err != nil {
    log.Fatal(err)
}

// 打开设备
if err := ts.Open(); err != nil {
    log.Fatal(err)
}
defer ts.Close()

// 读取触摸事件
for {
    point, err := ts.ReadTouch()
    if err != nil {
        continue
    }
    
    if point != nil {
        switch point.Event {
        case hardware.TouchPress:
            fmt.Printf("按下: (%d, %d)\n", point.X, point.Y)
        case hardware.TouchRelease:
            fmt.Printf("释放: (%d, %d)\n", point.X, point.Y)
        }
    }
}
```

### 3. 与UI集成

使用I2C触摸处理器：

```go
import "r1-toolbox/internal/ui"

// 创建I2C触摸处理器
touchHandler, err := ui.NewTouchHandlerWithI2C()
if err != nil {
    log.Fatal(err)
}
defer touchHandler.Close()

// 读取手势
for {
    gesture := touchHandler.ReadI2CTouch()
    switch gesture {
    case ui.GestureSwipeLeft:
        // 处理左滑
    case ui.GestureSwipeRight:
        // 处理右滑
    case ui.GestureTap:
        // 处理点击
    }
}
```

## 技术细节

### 初始化流程

1. 打开 `/dev/i2c-2` 设备
2. 使用 `ioctl` 设置I2C从设备地址为 0x2C
3. 写入初始化命令 `[0x00, 0x01]`
4. 读取30字节HID信息（包含Vendor ID、Product ID等）

### 数据格式

触摸数据包为64字节，格式如下：

```
偏移  | 说明
------|------------------
0-1   | 保留
2     | 0x20 (触摸点标记)
3     | 0x00
4     | 事件类型 & 0x0F
      |   0 = 释放
      |   1 = 按下
5-6   | X坐标 (小端序)
7-8   | Y坐标 (小端序)
```

### 坐标系统

- X轴: 0-376
- Y轴: 0-960
- 原点: 左上角

### 事件检测

- **按下**: 当检测到有效坐标且事件类型为1
- **释放**: 当事件类型为0，或连续2次读取失败

## 权限要求

访问I2C设备需要root权限或将用户添加到 `i2c` 组：

```bash
sudo usermod -a -G i2c $USER
```

## 故障排除

### 设备打开失败

确保I2C设备存在：
```bash
ls -l /dev/i2c-*
```

### 权限被拒绝

使用sudo运行或检查用户组权限：
```bash
groups $USER
```

### 无触摸响应

检查I2C设备地址：
```bash
sudo i2cdetect -y 2
```

应该在地址0x2C看到设备。
