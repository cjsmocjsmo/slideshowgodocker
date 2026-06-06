package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
)

type WeatherData struct {
	Temperature string    `json:"temperature"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	FetchedAt   time.Time `json:"fetchedAt"`
}

var (
	weatherMutex sync.RWMutex
	weatherCache WeatherData
)

const noaaPointsURL = "https://api.weather.gov/points/47.4502,-122.8276"

func fetchWeather() (WeatherData, error) {
	// Step 1: Get forecast URL from /points endpoint
	resp, err := http.Get(noaaPointsURL)
	if err != nil {
		return WeatherData{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return WeatherData{}, fmt.Errorf("NOAA points status: %d", resp.StatusCode)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return WeatherData{}, err
	}
	var pointsResp struct {
		Properties struct {
			Forecast string `json:"forecast"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body, &pointsResp); err != nil {
		return WeatherData{}, err
	}
	forecastURL := pointsResp.Properties.Forecast
	if forecastURL == "" {
		return WeatherData{}, fmt.Errorf("No forecast URL in NOAA points response")
	}

	// Step 2: Get forecast data from forecast URL
	resp2, err := http.Get(forecastURL)
	if err != nil {
		return WeatherData{}, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		return WeatherData{}, fmt.Errorf("NOAA forecast status: %d", resp2.StatusCode)
	}
	body2, err := ioutil.ReadAll(resp2.Body)
	if err != nil {
		return WeatherData{}, err
	}
	var apiResp struct {
		Properties struct {
			Periods []struct {
				Temperature   int    `json:"temperature"`
				ShortForecast string `json:"shortForecast"`
				Icon          string `json:"icon"`
			} `json:"periods"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(body2, &apiResp); err != nil {
		return WeatherData{}, err
	}
	if len(apiResp.Properties.Periods) == 0 {
		return WeatherData{}, nil
	}
	period := apiResp.Properties.Periods[0]
	return WeatherData{
		Temperature: fmt.Sprintf("%d°F", period.Temperature),
		Description: period.ShortForecast,
		Icon:        period.Icon,
		FetchedAt:   time.Now(),
	}, nil
}

func startWeatherUpdater() {
	go func() {
		for {
			weather, err := fetchWeather()
			if err == nil {
				weatherMutex.Lock()
				weatherCache = weather
				weatherMutex.Unlock()
			} else {
				log.Printf("Weather fetch error: %v", err)
			}
			time.Sleep(15 * time.Minute)
		}
	}()
}

func getWeatherHandler(w http.ResponseWriter, r *http.Request) {
	weatherMutex.RLock()
	weather := weatherCache
	weatherMutex.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(weather)
}
