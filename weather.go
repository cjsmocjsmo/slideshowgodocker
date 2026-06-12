package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
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

const (
	targetLat = 47.4502
	targetLon = -122.8276
)

var (
	maxStationsToCheck  int
	freshnessWindowMins float64
	distanceTieMiles    float64
)

func init() {
	// maxStationsToCheck: how many nearby stations to evaluate (default 10)
	if val := os.Getenv("WEATHER_MAX_STATIONS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			maxStationsToCheck = parsed
		} else {
			log.Printf("Warning: invalid WEATHER_MAX_STATIONS=%s, using default", val)
			maxStationsToCheck = 10
		}
	} else {
		maxStationsToCheck = 10
	}

	// freshnessWindowMins: prefer observations newer than this many minutes (default 90)
	if val := os.Getenv("WEATHER_FRESHNESS_MINS"); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			freshnessWindowMins = parsed
		} else {
			log.Printf("Warning: invalid WEATHER_FRESHNESS_MINS=%s, using default", val)
			freshnessWindowMins = 90.0
		}
	} else {
		freshnessWindowMins = 90.0
	}

	// distanceTieMiles: stations within this distance are considered equally close (default 5)
	if val := os.Getenv("WEATHER_DISTANCE_TIE_MILES"); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			distanceTieMiles = parsed
		} else {
			log.Printf("Warning: invalid WEATHER_DISTANCE_TIE_MILES=%s, using default", val)
			distanceTieMiles = 5.0
		}
	} else {
		distanceTieMiles = 5.0
	}

	log.Printf("Weather config: maxStations=%d, freshnessWindow=%.0f mins, distanceTie=%.1f miles", maxStationsToCheck, freshnessWindowMins, distanceTieMiles)
}

func httpGetJSON(url string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// NOAA recommends identifying requests with a user agent.
	req.Header.Set("User-Agent", "slideshowgodocker/1.0 (local app)")
	req.Header.Set("Accept", "application/geo+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("NOAA request failed for %s: status %d, body: %s", url, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, target); err != nil {
		return err
	}

	return nil
}

func celsiusToFahrenheit(c float64) int {
	return int(math.Round((c*9.0/5.0)+32.0))
}

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3958.8
	toRadians := func(deg float64) float64 {
		return deg * (math.Pi / 180.0)
	}

	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMiles * c
}

func fetchWeather() (WeatherData, error) {
	// Step 1: Get observation stations URL from /points endpoint.
	var pointsResp struct {
		Properties struct {
			ObservationStations string `json:"observationStations"`
		} `json:"properties"`
	}
	if err := httpGetJSON(noaaPointsURL, &pointsResp); err != nil {
		return WeatherData{}, err
	}
	stationsURL := pointsResp.Properties.ObservationStations
	if stationsURL == "" {
		return WeatherData{}, fmt.Errorf("No observation stations URL in NOAA points response")
	}

	// Step 2: Retrieve nearby station list.
	var stationsResp struct {
		Features []struct {
			Properties struct {
				StationIdentifier string `json:"stationIdentifier"`
			} `json:"properties"`
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := httpGetJSON(stationsURL, &stationsResp); err != nil {
		return WeatherData{}, err
	}
	if len(stationsResp.Features) == 0 {
		return WeatherData{}, fmt.Errorf("No observation stations found in NOAA stations response")
	}

	type stationCandidate struct {
		StationID    string
		DistanceMi   float64
		AgeMinutes   float64
		TemperatureF int
		Description  string
		Icon         string
		ObservedAt   time.Time
	}

	fetchLatestObservation := func(stationID string) (stationCandidate, bool) {
		latestObservationURL := fmt.Sprintf("https://api.weather.gov/stations/%s/observations/latest", stationID)
		var observationResp struct {
			Properties struct {
				Temperature struct {
					Value *float64 `json:"value"`
				} `json:"temperature"`
				TextDescription string `json:"textDescription"`
				Icon            string `json:"icon"`
				Timestamp       string `json:"timestamp"`
			} `json:"properties"`
		}
		if err := httpGetJSON(latestObservationURL, &observationResp); err != nil {
			log.Printf("Weather observation fetch failed for station %s: %v", stationID, err)
			return stationCandidate{}, false
		}

		if observationResp.Properties.Temperature.Value == nil || observationResp.Properties.Timestamp == "" {
			return stationCandidate{}, false
		}

		observedAt, err := time.Parse(time.RFC3339, observationResp.Properties.Timestamp)
		if err != nil {
			log.Printf("Weather timestamp parse failed for station %s: %v", stationID, err)
			return stationCandidate{}, false
		}

		description := observationResp.Properties.TextDescription
		if description == "" {
			description = "Current Conditions"
		}

		return stationCandidate{
			StationID:    stationID,
			TemperatureF: celsiusToFahrenheit(*observationResp.Properties.Temperature.Value),
			Description:  description,
			Icon:         observationResp.Properties.Icon,
			ObservedAt:   observedAt,
		}, true
	}

	// Step 3: Evaluate multiple stations and choose nearest with fresh data.
	now := time.Now()
	var candidates []stationCandidate
	checkCount := len(stationsResp.Features)
	if checkCount > maxStationsToCheck {
		checkCount = maxStationsToCheck
	}

	for i := 0; i < checkCount; i++ {
		feature := stationsResp.Features[i]
		stationID := feature.Properties.StationIdentifier
		if stationID == "" || len(feature.Geometry.Coordinates) < 2 {
			continue
		}

		candidate, ok := fetchLatestObservation(stationID)
		if !ok {
			continue
		}

		stationLon := feature.Geometry.Coordinates[0]
		stationLat := feature.Geometry.Coordinates[1]
		candidate.DistanceMi = haversineMiles(targetLat, targetLon, stationLat, stationLon)
		candidate.AgeMinutes = now.Sub(candidate.ObservedAt).Minutes()
		if candidate.AgeMinutes < 0 {
			candidate.AgeMinutes = 0
		}

		candidates = append(candidates, candidate)
	}

	if len(candidates) == 0 {
		return WeatherData{}, fmt.Errorf("No stations returned valid current temperature observations")
	}

	selectBest := func(pool []stationCandidate) stationCandidate {
		best := pool[0]
		for i := 1; i < len(pool); i++ {
			current := pool[i]
			if current.DistanceMi < best.DistanceMi-distanceTieMiles {
				best = current
				continue
			}
			if math.Abs(current.DistanceMi-best.DistanceMi) <= distanceTieMiles && current.AgeMinutes < best.AgeMinutes {
				best = current
			}
		}
		return best
	}

	var freshCandidates []stationCandidate
	for _, c := range candidates {
		if c.AgeMinutes <= freshnessWindowMins {
			freshCandidates = append(freshCandidates, c)
		}
	}

	best := stationCandidate{}
	if len(freshCandidates) > 0 {
		best = selectBest(freshCandidates)
	} else {
		best = selectBest(candidates)
	}

	log.Printf("Weather station selected: %s (distance %.1f mi, age %.0f min)", best.StationID, best.DistanceMi, best.AgeMinutes)

	return WeatherData{
		Temperature: fmt.Sprintf("%d°F", best.TemperatureF),
		Description: best.Description,
		Icon:        best.Icon,
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
				broadcastWeather(weather)
			} else {
				log.Printf("Weather fetch error: %v", err)
			}
			time.Sleep(5 * time.Minute)
		}
	}()
}

func getCurrentWeatherData() (WeatherData, bool) {
	weatherMutex.RLock()
	weather := weatherCache
	weatherMutex.RUnlock()

	if weather.FetchedAt.IsZero() {
		return WeatherData{}, false
	}

	return weather, true
}

func getWeatherHandler(w http.ResponseWriter, r *http.Request) {
	weatherMutex.RLock()
	weather := weatherCache
	weatherMutex.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(weather)
}
