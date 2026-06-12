package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type ImageData struct {
	Name        string
	Path        string
	Http        string
	Idx         int
	Orientation string
}

func dbCount() int {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("Error opening count database: %v", err)
		return 0
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM images").Scan(&count)
	if err != nil {
		log.Printf("Error querying count: %v", err)
		return 0
	}
	return count
}

func getAvailableIndices() []int {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("Error opening database for indices: %v", err)
		return []int{}
	}
	defer db.Close()

	query := "SELECT idx FROM images ORDER BY idx"
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Error querying indices: %v", err)
		return []int{}
	}
	defer rows.Close()

	var indices []int
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			log.Printf("Error scanning index: %v", err)
			continue
		}
		indices = append(indices, idx)
	}

	log.Printf("Available indices: %v", indices)
	return indices
}

func getDBImage(idx int) (ImageData, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("Error opening database: %v", err)
		return ImageData{}, err
	}
	defer db.Close()

	var img ImageData
	query := "SELECT name, http, idx, orientation FROM images WHERE idx = ?"
	err = db.QueryRow(query, idx).Scan(&img.Name, &img.Http, &img.Idx, &img.Orientation)
	if err != nil {
		log.Printf("Error querying getDBImage: %v", err)
		return ImageData{}, err
	}
	return img, nil
}

func getCurrentImageData() (ImageData, error) {
	imageMutex.RLock()
	idx := currentImageIdx
	hasImages := len(availableIndices) > 0
	imageMutex.RUnlock()

	if !hasImages {
		return ImageData{}, fmt.Errorf("no images available")
	}

	return getDBImage(idx)
}

func advanceSlideshowAndBroadcast() {
	imageMutex.Lock()
	if len(availableIndices) == 0 {
		imageMutex.Unlock()
		log.Printf("Slideshow tick skipped: no images available")
		return
	}

	currentSlideIndex++
	if currentSlideIndex >= len(availableIndices) {
		currentSlideIndex = 0
	}
	currentImageIdx = availableIndices[currentSlideIndex]
	idx := currentImageIdx
	position := currentSlideIndex + 1
	total := len(availableIndices)
	imageMutex.Unlock()

	log.Printf("Slideshow advanced to image index %d (position %d of %d)", idx, position, total)

	data, err := getDBImage(idx)
	if err != nil {
		log.Printf("Error loading slideshow image for broadcast: %v", err)
		return
	}

	broadcastSlide(data)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	imageMutex.RLock()
	idx := currentImageIdx
	imageMutex.RUnlock()

	fmt.Printf("Available indices: %v, dbCount: %d, currentIdx: %d, slidePosition: %d\n",
		availableIndices, dbCountValue, idx, currentSlideIndex+1)

	if len(availableIndices) == 0 {
		log.Printf("No images available in database")
		http.Error(w, "No images available", http.StatusInternalServerError)
		return
	}

	data, err := getDBImage(idx)
	if err != nil {
		log.Printf("Error getting image from database: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err = templates.ExecuteTemplate(w, "index.html", data)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

// getCurrentImageJSON returns the current image data as JSON
func getCurrentImageJSON(w http.ResponseWriter, r *http.Request) {
	data, err := getCurrentImageData()
	if err != nil {
		http.Error(w, "No images available", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// helloWorldHandler returns a simple "Hello World" message
func helloWorldHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Hello World"))
}

func withMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

// serveStaticFiles sets up a file server for static assets (like CSS, JS, images).
func serveStaticFiles(router *http.ServeMux) {
	staticFileServer := http.FileServer(http.Dir(imageDir))
	router.Handle("/static/", http.StripPrefix("/static/", staticFileServer))
}
