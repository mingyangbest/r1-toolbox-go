package web

import (
	"github.com/gin-gonic/gin"

	"r1-toolbox/internal/config"
	"r1-toolbox/internal/hardware"
	"r1-toolbox/internal/monitor"
)

type Server struct {
	router      *gin.Engine
	monitor     *monitor.SystemMonitor
	fanCtrl     *hardware.FanController
	rgbCtrl     *hardware.RGBController
	brightness  *hardware.BrightnessController
	config      *config.Config
	uiRenderer  interface{ LoadBackground(string) error; SetTextColor(string) }
}

func NewServer(mon *monitor.SystemMonitor, fan *hardware.FanController, rgb *hardware.RGBController, bright *hardware.BrightnessController, cfg *config.Config, uiRenderer interface{ LoadBackground(string) error; SetTextColor(string) }) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	
	s := &Server{
		router:     router,
		monitor:    mon,
		fanCtrl:    fan,
		rgbCtrl:    rgb,
		brightness: bright,
		config:     cfg,
		uiRenderer: uiRenderer,
	}
	
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Next()
	})
	
	distFS := getDistFS()
	s.router.GET("/assets/*filepath", func(c *gin.Context) {
		c.FileFromFS(c.Request.URL.Path, distFS)
	})
	
	s.router.GET("/", s.handleIndex)
	s.router.GET("/api/status", s.handleStatus)
	s.router.GET("/api/hwmon/scan", s.handleHwmonScan)
	s.router.POST("/api/brightness", s.handleBrightness)
	s.router.POST("/api/screen/timeout", s.handleScreenTimeout)
	s.router.POST("/api/screen/off", s.handleScreenOff)
	s.router.POST("/api/screen/on", s.handleScreenOn)
	s.router.POST("/api/background", s.handleBackground)
	s.router.POST("/api/textcolor", s.handleTextColor)
	s.router.POST("/api/rgb", s.handleRGB)
	s.router.POST("/api/fan", s.handleFan)
	s.router.POST("/api/fan/curve", s.handleFanCurve)
	s.router.POST("/api/fan/mapping", s.handleFanMapping)
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}


func (s *Server) handleIndex(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	htmlContent, _ := distFS.ReadFile("dist/index.html")
	if len(htmlContent) > 0 {
		c.Data(200, "text/html; charset=utf-8", htmlContent)
	} else {
		c.String(200, htmlTemplate)
	}
}

func (s *Server) handleStatus(c *gin.Context) {
	bright, _ := s.brightness.Get()
	cpuTemp := s.fanCtrl.GetTemp(s.config.Fan.CPU.TempSensor)
	hddTemp := s.fanCtrl.GetTemp(s.config.Fan.HDD.TempSensor)
	
	c.JSON(200, gin.H{
		"cpu":        s.monitor.CPUPercent,
		"cpu_temp":   cpuTemp,
		"hdd_temp":   hddTemp,
		"memory":     s.monitor.MemPercent,
		"brightness": bright,
	})
}

func (s *Server) handleHwmonScan(c *gin.Context) {
	scanner := hardware.NewHwmonScanner(s.config.Hardware.HwmonPath)
	fans := scanner.ScanFans()
	temps := scanner.ScanTemps()
	
	c.JSON(200, gin.H{
		"fans":  fans,
		"temps": temps,
	})
}

func (s *Server) handleScreenOff(c *gin.Context) {
	s.brightness.Set(0)
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleBrightness(c *gin.Context) {
	var req struct {
		Value int `json:"value"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	s.brightness.Set(req.Value)
	s.config.Screen.Brightness = req.Value
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleScreenTimeout(c *gin.Context) {
	var req struct {
		Timeout int `json:"timeout"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	s.config.Screen.Timeout = req.Timeout
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleRGB(c *gin.Context) {
	var req struct {
		Mode  string `json:"mode"`
		Color string `json:"color"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	s.rgbCtrl.SetMode(req.Mode, req.Color)
	s.config.RGB.Mode = req.Mode
	s.config.RGB.Color = req.Color
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleFan(c *gin.Context) {
	var req struct {
		Type string `json:"type"`
		Mode string `json:"mode"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Type == "cpu" {
		s.fanCtrl.SetCPUMode(req.Mode)
		s.config.Fan.CPU.Mode = req.Mode
	} else {
		s.fanCtrl.SetHDDMode(req.Mode)
		s.config.Fan.HDD.Mode = req.Mode
	}
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleFanCurve(c *gin.Context) {
	var req struct {
		Type    string `json:"type"`
		MinTemp int    `json:"min_temp"`
		MaxTemp int    `json:"max_temp"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Type == "cpu" {
		s.fanCtrl.SetCPUTempRange(req.MinTemp, req.MaxTemp)
		s.config.Fan.CPU.MinTemp = req.MinTemp
		s.config.Fan.CPU.MaxTemp = req.MaxTemp
	} else {
		s.fanCtrl.SetHDDTempRange(req.MinTemp, req.MaxTemp)
		s.config.Fan.HDD.MinTemp = req.MinTemp
		s.config.Fan.HDD.MaxTemp = req.MaxTemp
	}
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}


func (s *Server) handleFanMapping(c *gin.Context) {
	var req struct {
		Type       string `json:"type"`
		PWMIndex   int    `json:"pwm_index"`
		TempSensor string `json:"temp_sensor"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Type == "cpu" {
		s.config.Fan.CPU.PWMIndex = req.PWMIndex
		s.config.Fan.CPU.TempSensor = req.TempSensor
	} else {
		s.config.Fan.HDD.PWMIndex = req.PWMIndex
		s.config.Fan.HDD.TempSensor = req.TempSensor
	}
	s.config.Save()
	c.JSON(200, gin.H{"success": true})
}


func (s *Server) handleScreenOn(c *gin.Context) {
	var req struct {
		Value int `json:"value"`
	}
	if err := c.BindJSON(&req); err == nil && req.Value > 0 {
		s.brightness.Set(req.Value)
	} else {
		s.brightness.Set(36000)
	}
	c.JSON(200, gin.H{"success": true})
}


func (s *Server) handleBackground(c *gin.Context) {
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	
	path := "/etc/r1-toolbox/background.jpg"
	if err := c.SaveUploadedFile(file, path); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	
	s.config.Screen.Background = path
	s.config.Save()
	
	if s.uiRenderer != nil {
		s.uiRenderer.LoadBackground(path)
	}
	
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleTextColor(c *gin.Context) {
	var req struct {
		Color string `json:"color"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	
	s.config.Screen.TextColor = req.Color
	s.config.Save()
	
	if s.uiRenderer != nil {
		s.uiRenderer.SetTextColor(req.Color)
	}
	
	c.JSON(200, gin.H{"success": true})
}
