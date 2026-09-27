package web

import (
	"strings"

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
		// /api/* 全部要求令牌（X-Token 头或 token 查询参数）。
		// 页面外壳（/ 与 /assets）保持开放 —— 它不含任何数据，
		// 浏览器首次访问时由内嵌脚本引导输入令牌。
		if strings.HasPrefix(c.Request.URL.Path, "/api/") &&
			c.GetHeader("X-Token") != s.config.Web.Token &&
			c.Query("token") != s.config.Web.Token {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
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


// tokenBootstrap 注入到 index.html 的 fetch 拦截器：
// 所有请求自动带上 localStorage 里的令牌；收到 401 时弹出输入框
// （令牌见设备屏幕「系统」页），输入成功即重试。首个请求为 GET 时
// 还能把令牌换进 URL，刷新后依然有效。
const tokenBootstrap = `<script>
(function(){
  var KEY='r1tb_token';
  var orig=window.fetch.bind(window);
  window.fetch=function(input,init){
    init=init||{};
    var h=new Headers(init.headers||{});
    h.set('X-Token',localStorage.getItem(KEY)||'');
    return orig(input,Object.assign({},init,{headers:h})).then(function(r){
      if(r.status!==401) return r;
      var t=prompt('首次使用：请输入设备屏幕「系统」页显示的面板令牌');
      if(!t) return r;
      localStorage.setItem(KEY,t.trim());
      return window.fetch(input,init);
    });
  };
})();
</script>`

func withTokenBootstrap(html string) string {
	if i := strings.Index(html, "</head>"); i >= 0 {
		return html[:i] + tokenBootstrap + html[i:]
	}
	return html + tokenBootstrap
}

func (s *Server) handleIndex(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	htmlContent, _ := distFS.ReadFile("dist/index.html")
	if len(htmlContent) > 0 {
		c.Data(200, "text/html; charset=utf-8", []byte(withTokenBootstrap(string(htmlContent))))
	} else {
		c.String(200, withTokenBootstrap(htmlTemplate))
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
	// 🔴 入参校验（2026-09-27 修复）：越界值曾被原样写入配置持久化 ——
	// 实测 {"value":-500} 会把屏幕熄灭且重启后仍为 0 亮度（自残状态）。
	// 合法域 [0, 硬件上限]，上限来自 max_brightness，不做硬编码。
	if req.Value < 0 || req.Value > s.brightness.Max() {
		c.JSON(400, gin.H{"error": "value out of range"})
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
	if req.Timeout < 0 {
		c.JSON(400, gin.H{"error": "timeout must be >= 0"})
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
	// 曲线自洽性：min 必须小于 max，且都落在物理合理域 [0,120]°C。
	// 修复前 {"min_temp":90,"max_temp":30} 会被照单全收，调速行为未定义。
	if req.MinTemp < 0 || req.MinTemp > 120 || req.MaxTemp < 0 || req.MaxTemp > 120 || req.MinTemp >= req.MaxTemp {
		c.JSON(400, gin.H{"error": "invalid curve: require 0 <= min_temp < max_temp <= 120"})
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
