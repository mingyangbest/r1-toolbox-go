// Package netinfo 负责获取外网信息：
//   - 公网 IP + 中文归属地 + 运营商（myip.ipip.net）
//   - 当前天气：优先心知天气（原生中文城市与描述，实时温度 + 今日高低温/湿度），
//     失败自动回退 wttr.in
//
// 数据由后台 goroutine 定时刷新，失败自动短间隔重试；
// 界面侧只读缓存，网络请求永不阻塞渲染。
package netinfo

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// 心知天气（实时 + 今日预报）
	senNowURL   = "https://api.seniverse.com/v3/weather/now.json?key=%s&location=ip&language=zh-Hans&unit=c"
	senDailyURL = "https://api.seniverse.com/v3/weather/daily.json?key=%s&location=ip&language=zh-Hans&unit=c&start=0&days=1"
	senKey      = "SWcgQfk4sHr8uPDYc"

	// 兜底：wttr.in
	wttrURL = "https://wttr.in/?format=j1"

	ipURL = "https://myip.ipip.net"

	userAgent = "r1-toolbox/1.0 (frontscreen)"

	weatherEvery = 30 * time.Minute // 洋哥拍板：静态图标 + 半小时刷一次
	ipEvery      = 30 * time.Minute
	retryEvery   = 45 * time.Second
	httpTimeout  = 10 * time.Second
)

// Weather 当前天气快照
type Weather struct {
	TempC    float64 // 当前温度
	MaxC     float64 // 今日最高
	MinC     float64 // 今日最低
	Humidity int
	WindDir  string // 西北
	WindText string // 2 级
	Desc     string // 多云
	Kind     string // sun/cloud/overcast/rain/snow/fog/thunder
	Code     int    // 数据源原始天气代码（仅兜底源使用）
	City     string // 苏州
	Src      string // seniverse / wttr
	OK       bool
	At       time.Time
}

// PublicIP 公网出口信息
type PublicIP struct {
	IP       string
	Location string // 中国 江苏 苏州
	City     string // 苏州
	ISP      string // 联通
	OK       bool
	At       time.Time
}

// Service 外网信息缓存 + 定时刷新
type Service struct {
	mu  sync.RWMutex
	w   Weather
	p   PublicIP
	cli *http.Client
}

func New() *Service {
	return &Service{cli: &http.Client{Timeout: httpTimeout}}
}

func (s *Service) Weather() Weather {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.w
}

func (s *Service) PublicIP() PublicIP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.p
}

// Start 启动后台刷新（非阻塞）
func (s *Service) Start() { go s.loop() }

// loop 简单的时间轮：每 5 秒看一次是否到刷新点
func (s *Service) loop() {
	wNext, iNext := time.Now(), time.Now()
	for {
		now := time.Now()
		if !now.Before(wNext) {
			if s.refreshWeather() {
				wNext = now.Add(weatherEvery)
			} else {
				wNext = now.Add(retryEvery)
			}
		}
		if !now.Before(iNext) {
			if s.refreshIP() {
				iNext = now.Add(ipEvery)
			} else {
				iNext = now.Add(retryEvery)
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func (s *Service) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// ------------------------------------------------------------
//  公网 IP
// ------------------------------------------------------------

func (s *Service) refreshIP() bool {
	b, err := s.get(ipURL)
	if err != nil {
		log.Printf("[netinfo] 公网IP 获取失败: %v", err)
		return false
	}
	body := string(b)
	ip := findIPv4(body)
	if ip == "" {
		log.Printf("[netinfo] 公网IP 解析失败: %q", truncate(body, 120))
		return false
	}
	loc, isp := "", ""
	for _, sep := range []string{"来自于：", "来自于:"} {
		if i := strings.Index(body, sep); i >= 0 {
			f := strings.Fields(body[i+len(sep):])
			if len(f) > 0 {
				isp = f[len(f)-1]
				loc = strings.Join(f[:len(f)-1], " ")
			}
			break
		}
	}
	city := ""
	if fs := strings.Fields(loc); len(fs) > 0 {
		city = fs[len(fs)-1]
	}
	s.mu.Lock()
	s.p = PublicIP{IP: ip, Location: strings.TrimSpace(loc), City: city, ISP: isp, OK: true, At: time.Now()}
	s.mu.Unlock()
	log.Printf("[netinfo] 公网IP: %s  (%s %s)", ip, loc, isp)
	return true
}

// findIPv4 从任意文本里抽出第一个合法 IPv4
func findIPv4(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r == '.')
	})
	for _, f := range fields {
		f = strings.Trim(f, ".")
		if isIPv4(f) {
			return f
		}
	}
	return ""
}

func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------
//  天气：主源心知天气，回退 wttr.in
// ------------------------------------------------------------

func (s *Service) refreshWeather() bool {
	if w, ok := s.fetchSeniverse(); ok {
		s.storeWeather(w)
		return true
	}
	if w, ok := s.fetchWttr(); ok {
		s.storeWeather(w)
		return true
	}
	log.Printf("[netinfo] 天气获取失败（两个数据源均不可用）")
	return false
}

func (s *Service) storeWeather(w Weather) {
	// 城市名优先用 IP 归属地（更准，且去掉「市」字）
	s.mu.RLock()
	city := s.p.City
	s.mu.RUnlock()
	if city != "" {
		w.City = city
	}
	if w.City == "" {
		w.City = "本地"
	}
	w.City = strings.ReplaceAll(w.City, "市", "")
	w.OK = true
	w.At = time.Now()

	s.mu.Lock()
	s.w = w
	s.mu.Unlock()
	log.Printf("[netinfo] 天气[%s]: %s %.0f°C 今日%.0f~%.0f°C 湿度%d%% (%s) @%s",
		w.Src, w.Desc, w.TempC, w.MinC, w.MaxC, w.Humidity, w.Kind, w.City)
}

type senNowResp struct {
	Results []struct {
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
		Now struct {
			Text string `json:"text"`
			Code string `json:"code"`
			Temp string `json:"temperature"`
		} `json:"now"`
	} `json:"results"`
}

type senDailyResp struct {
	Results []struct {
		Daily []struct {
			TextDay   string `json:"text_day"`
			High      string `json:"high"`
			Low       string `json:"low"`
			Humidity  string `json:"humidity"`
			WindDir   string `json:"wind_direction"`
			WindScale string `json:"wind_scale"`
		} `json:"daily"`
	} `json:"results"`
}

func (s *Service) fetchSeniverse() (Weather, bool) {
	b, err := s.get(sprintf(senNowURL, senKey))
	if err != nil {
		log.Printf("[netinfo] 心知天气(实时) 请求失败: %v", err)
		return Weather{}, false
	}
	var nr senNowResp
	if err := json.Unmarshal(b, &nr); err != nil || len(nr.Results) == 0 {
		log.Printf("[netinfo] 心知天气(实时) 解析失败: %v", err)
		return Weather{}, false
	}
	res := nr.Results[0]
	w := Weather{
		TempC: atof(res.Now.Temp),
		Desc:  strings.TrimSpace(res.Now.Text),
		City:  res.Location.Name,
		Src:   "seniverse",
	}
	w.Kind = kindOf(w.Desc)

	// 今日高低温 / 湿度 / 风（失败不影响主信息）
	if b2, err2 := s.get(sprintf(senDailyURL, senKey)); err2 == nil {
		var dr senDailyResp
		if json.Unmarshal(b2, &dr) == nil && len(dr.Results) > 0 && len(dr.Results[0].Daily) > 0 {
			d := dr.Results[0].Daily[0]
			w.MaxC = atof(d.High)
			w.MinC = atof(d.Low)
			w.Humidity = atoi(d.Humidity)
			w.WindDir = strings.TrimSpace(d.WindDir)
			if sc := strings.TrimSpace(d.WindScale); sc != "" {
				w.WindText = sc + " 级"
			}
		}
	}
	return w, true
}

type wttrResp struct {
	Current []struct {
		TempC       string `json:"temp_C"`
		Humidity    string `json:"humidity"`
		WindKmph    string `json:"windspeedKmph"`
		WeatherCode string `json:"weatherCode"`
	} `json:"current_condition"`
	Nearest struct {
		AreaName []struct{ Value string } `json:"areaName"`
	} `json:"nearest_area"`
	Weather []struct {
		MaxC string `json:"maxtempC"`
		MinC string `json:"mintempC"`
	} `json:"weather"`
}

func (s *Service) fetchWttr() (Weather, bool) {
	b, err := s.get(wttrURL)
	if err != nil {
		log.Printf("[netinfo] wttr.in 请求失败: %v", err)
		return Weather{}, false
	}
	var r wttrResp
	if err := json.Unmarshal(b, &r); err != nil || len(r.Current) == 0 {
		log.Printf("[netinfo] wttr.in 解析失败: %v", err)
		return Weather{}, false
	}
	cur := r.Current[0]
	w := Weather{
		TempC:    atof(cur.TempC),
		Humidity: atoi(cur.Humidity),
		Code:     atoi(cur.WeatherCode),
		Src:      "wttr",
	}
	w.Desc, w.Kind = describeWWO(w.Code)
	if len(r.Weather) > 0 {
		w.MaxC = atof(r.Weather[0].MaxC)
		w.MinC = atof(r.Weather[0].MinC)
	}
	if len(r.Nearest.AreaName) > 0 {
		w.City = r.Nearest.AreaName[0].Value
	}
	return w, true
}

// kindOf 由中文天气描述判断动画类型。
// 刻意不依赖天气代码 —— 各家的代码表并不一致，而中文描述本身是准的。
func kindOf(text string) string {
	switch {
	case strings.Contains(text, "雷"):
		return "thunder"
	case strings.Contains(text, "雪"), strings.Contains(text, "冰雹"):
		return "snow"
	case strings.Contains(text, "雨"):
		return "rain"
	case strings.Contains(text, "雾"), strings.Contains(text, "霾"),
		strings.Contains(text, "尘"), strings.Contains(text, "沙"):
		return "fog"
	case strings.Contains(text, "阴"):
		return "overcast"
	case strings.Contains(text, "云"):
		return "cloud"
	case strings.Contains(text, "晴"):
		return "sun"
	}
	return "cloud"
}

// describeWWO 把 wttr.in 的 WWO 天气代码映射为中文（仅兜底源使用）
func describeWWO(code int) (string, string) {
	switch code {
	case 113:
		return "晴", "sun"
	case 116:
		return "多云", "cloud"
	case 119, 122:
		return "阴", "overcast"
	case 143, 248, 260:
		return "雾", "fog"
	case 200, 386, 389, 392, 395:
		return "雷雨", "thunder"
	case 179, 182, 185, 317, 320, 350, 362, 365, 374, 377:
		return "雨夹雪", "snow"
	case 227, 230, 323, 326, 329, 332, 335, 338, 368, 371:
		return "雪", "snow"
	case 176, 263, 266, 293, 296, 299, 353, 356, 359:
		return "阵雨", "rain"
	case 302, 305, 308, 311, 314:
		return "雨", "rain"
	}
	switch {
	case code >= 386:
		return "雷雨", "thunder"
	case code >= 317 && code <= 338, code >= 368:
		return "雪", "snow"
	case code >= 293 && code <= 314:
		return "雨", "rain"
	}
	return "多云", "cloud"
}

// ------------------------------------------------------------
//  小工具
// ------------------------------------------------------------

// sprintf 只在 netinfo 内部用于拼 URL，避免把 fmt 引进热路径之外的判断
func sprintf(format, arg string) string {
	return strings.Replace(format, "%s", arg, 1)
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func atoi(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
