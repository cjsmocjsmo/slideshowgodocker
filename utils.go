package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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

const (
	weatherConfigPath      = "config/weather.json"
	defaultWeatherLocation = "Walla Walla, WA"
	defaultWeatherLat      = 46.0646
	defaultWeatherLon      = -118.3430
)

type weatherConfig struct {
	Location  string   `json:"location"`
	Latitude  *float64 `json:"lat"`
	Longitude *float64 `json:"lon"`
}

var (
	maxStationsToCheck  int
	freshnessWindowMins float64
	distanceTieMiles    float64
	weatherLocation     string
	weatherTargetLat    float64
	weatherTargetLon    float64
	weatherPointsURL    string
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

	configureWeatherLocation()

	log.Printf("Weather config: maxStations=%d, freshnessWindow=%.0f mins, distanceTie=%.1f miles", maxStationsToCheck, freshnessWindowMins, distanceTieMiles)
}

func configureWeatherLocation() {
	location, lat, lon, hasCoordinates, usedDefault, reason := readWeatherLocationConfig()
	if usedDefault {
		log.Printf("Weather location fallback to %q (%s)", defaultWeatherLocation, reason)
	}

	if !hasCoordinates {
		resolvedLat, resolvedLon, err := geocodeLocation(location)
		if err != nil {
			if !strings.EqualFold(location, defaultWeatherLocation) {
				log.Printf("Weather location geocode failed for %q: %v. Falling back to %q", location, err, defaultWeatherLocation)
				location = defaultWeatherLocation
				resolvedLat, resolvedLon, err = geocodeLocation(location)
			}

			if err != nil {
				log.Printf("Weather geocode failed for default location %q: %v. Using built-in coordinates", defaultWeatherLocation, err)
				resolvedLat = defaultWeatherLat
				resolvedLon = defaultWeatherLon
			}
		}

		lat = resolvedLat
		lon = resolvedLon
	}

	weatherLocation = location
	weatherTargetLat = lat
	weatherTargetLon = lon
	weatherPointsURL = fmt.Sprintf("https://api.weather.gov/points/%.4f,%.4f", weatherTargetLat, weatherTargetLon)

	log.Printf("Weather location in use: %q (lat=%.4f, lon=%.4f)", weatherLocation, weatherTargetLat, weatherTargetLon)
}

func readWeatherLocationConfig() (string, float64, float64, bool, bool, string) {
	data, err := os.ReadFile(weatherConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, fmt.Sprintf("%s not found", weatherConfigPath)
		}
		return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, fmt.Sprintf("unable to read %s: %v", weatherConfigPath, err)
	}

	if strings.TrimSpace(string(data)) == "" {
		return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, "config file is empty"
	}

	var cfg weatherConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, fmt.Sprintf("invalid JSON: %v", err)
	}

	location := strings.TrimSpace(cfg.Location)
	if location == "" {
		return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, "location is missing or empty"
	}

	if cfg.Latitude != nil && cfg.Longitude != nil {
		if !isValidLatitude(*cfg.Latitude) || !isValidLongitude(*cfg.Longitude) {
			return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, "lat/lon out of valid range"
		}
		return location, *cfg.Latitude, *cfg.Longitude, true, false, ""
	}

	if (cfg.Latitude != nil) != (cfg.Longitude != nil) {
		return defaultWeatherLocation, defaultWeatherLat, defaultWeatherLon, true, true, "lat/lon must both be provided"
	}

	return location, 0, 0, false, false, ""
}

func isValidLatitude(lat float64) bool {
	return lat >= -90.0 && lat <= 90.0
}

func isValidLongitude(lon float64) bool {
	return lon >= -180.0 && lon <= 180.0
}

func geocodeLocation(location string) (float64, float64, error) {
	endpoint := "https://nominatim.openstreetmap.org/search?format=json&limit=1&q=" + url.QueryEscape(location)

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, err
	}

	req.Header.Set("User-Agent", "slideshowgodocker/1.0 (local app)")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, 0, fmt.Errorf("geocode request failed: status %d, body: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return 0, 0, err
	}

	if len(results) == 0 {
		return 0, 0, fmt.Errorf("no geocode results")
	}

	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid latitude %q", results[0].Lat)
	}

	lon, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid longitude %q", results[0].Lon)
	}

	return lat, lon, nil
}

func httpGetJSON(url string, target interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// NOAA recommends identifying requests with a user agent.
	req.Header.Set("User-Agent", "slideshowgodocker/1.0 (local app)")
	req.Header.Set("Accept", "application/geo+json, application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed for %s: status %d, body: %s", url, resp.StatusCode, string(body))
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
	return int(math.Round((c * 9.0 / 5.0) + 32.0))
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
	if err := httpGetJSON(weatherPointsURL, &pointsResp); err != nil {
		return WeatherData{}, err
	}
	stationsURL := pointsResp.Properties.ObservationStations
	if stationsURL == "" {
		return WeatherData{}, fmt.Errorf("no observation stations URL in NOAA points response")
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
		return WeatherData{}, fmt.Errorf("no observation stations found in NOAA stations response")
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
		candidate.DistanceMi = haversineMiles(weatherTargetLat, weatherTargetLon, stationLat, stationLon)
		candidate.AgeMinutes = now.Sub(candidate.ObservedAt).Minutes()
		if candidate.AgeMinutes < 0 {
			candidate.AgeMinutes = 0
		}

		candidates = append(candidates, candidate)
	}

	if len(candidates) == 0 {
		return WeatherData{}, fmt.Errorf("no stations returned valid current temperature observations")
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

	log.Printf("Weather station selected: %s for %q (distance %.1f mi, age %.0f min)", best.StationID, weatherLocation, best.DistanceMi, best.AgeMinutes)

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

const musicConfigPath = "config/music.json"

type MusicSource struct {
	Src  string `json:"src"`
	Type string `json:"type"`
}

type MusicConfig struct {
	Name    string        `json:"name"`
	Sources []MusicSource `json:"sources"`
}

var (
	musicConfigMutex sync.RWMutex
	musicConfigCache = defaultMusicConfig()
)

func defaultMusicConfig() MusicConfig {
	return MusicConfig{
		Name: "HANK FM Seattle",
		Sources: []MusicSource{
			{Src: "https://playerservices.streamtheworld.com/api/livestream-redirect/KPLZFM.mp3", Type: "audio/mpeg"},
			{Src: "https://playerservices.streamtheworld.com/api/livestream-redirect/KPLZFMAAC.aac", Type: "audio/aac"},
			{Src: "https://ice42.securenetsystems.net/KPLZ", Type: "audio/mpeg"},
		},
	}
}

func loadMusicConfig() {
	cfg, usedDefault, reason := readMusicConfig()

	musicConfigMutex.Lock()
	musicConfigCache = cfg
	musicConfigMutex.Unlock()

	if usedDefault {
		log.Printf("Music config fallback to built-in default (%s)", reason)
	}
	log.Printf("Music station in use: %q (%d source(s))", cfg.Name, len(cfg.Sources))
}

func getMusicConfig() MusicConfig {
	musicConfigMutex.RLock()
	defer musicConfigMutex.RUnlock()

	sources := make([]MusicSource, 0, len(musicConfigCache.Sources))
	sources = append(sources, musicConfigCache.Sources...)

	return MusicConfig{
		Name:    musicConfigCache.Name,
		Sources: sources,
	}
}

func readMusicConfig() (MusicConfig, bool, string) {
	data, err := os.ReadFile(musicConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultMusicConfig(), true, fmt.Sprintf("%s not found", musicConfigPath)
		}
		return defaultMusicConfig(), true, fmt.Sprintf("unable to read %s: %v", musicConfigPath, err)
	}

	if strings.TrimSpace(string(data)) == "" {
		return defaultMusicConfig(), true, "config file is empty"
	}

	var cfg MusicConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultMusicConfig(), true, fmt.Sprintf("invalid JSON: %v", err)
	}

	cleanedSources := make([]MusicSource, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		cleanSrc := strings.TrimSpace(src.Src)
		cleanType := strings.TrimSpace(src.Type)
		if cleanSrc == "" || cleanType == "" {
			continue
		}
		cleanedSources = append(cleanedSources, MusicSource{Src: cleanSrc, Type: cleanType})
	}

	if len(cleanedSources) == 0 {
		return defaultMusicConfig(), true, "no valid sources configured"
	}

	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "Configured Station"
	}

	return MusicConfig{Name: name, Sources: cleanedSources}, false, ""
}
