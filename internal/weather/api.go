package weather

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const APIKey = "SWcgQfk4sHr8uPDYc"

type WeatherInfo struct {
	City      string
	Temp      int
	TempLow   int
	Condition string
	Humidity  int
	Code      int
	Forecast  []ForecastDay
}

type ForecastDay struct {
	Date      string
	Weekday   string
	TempHigh  int
	TempLow   int
	Condition string
	Code      int
}

func GetWeather() (*WeatherInfo, error) {
	url := fmt.Sprintf("https://api.seniverse.com/v3/weather/daily.json?key=%s&location=ip&language=zh-Hans&unit=c", APIKey)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		Results []struct {
			Location struct {
				Name string `json:"name"`
			} `json:"location"`
			Daily []struct {
				Date          string `json:"date"`
				High          string `json:"high"`
				Low           string `json:"low"`
				TextDay       string `json:"text_day"`
				CodeDay       string `json:"code_day"`
				Humidity      string `json:"humidity"`
			} `json:"daily"`
		} `json:"results"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	if len(result.Results) == 0 || len(result.Results[0].Daily) == 0 {
		return nil, fmt.Errorf("无天气数据")
	}
	
	info := &WeatherInfo{City: result.Results[0].Location.Name}
	today := result.Results[0].Daily[0]
	
	fmt.Sscanf(today.High, "%d", &info.Temp)
	fmt.Sscanf(today.Low, "%d", &info.TempLow)
	fmt.Sscanf(today.CodeDay, "%d", &info.Code)
	fmt.Sscanf(today.Humidity, "%d", &info.Humidity)
	info.Condition = today.TextDay
	
	return info, nil
}
