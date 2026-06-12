package main

import (
	"fmt"
	"html/template"
	"log"
	_ "modernc.org/sqlite"
	"net/http"
	"sync"
	"time"
)

// Global variable to store parsed templates
var templates *template.Template
var dbPath = "/app/DB/imagesDB"
var imageDir = "/app/test2/"

// Global variables for slideshow control
var currentImageIdx int = 1
var imageMutex sync.RWMutex
var availableIndices []int

var dbCountValue = dbCount()
var currentSlideIndex = 0 // Index into availableIndices array

func init() {
	// Parse all templates in the "templates" directory.
	// template.Must panics if there's an error, which is good for quick startup
	// errors for templates. In a larger app, you might handle errors more gracefully.
	templates = template.Must(template.ParseGlob("templates/*.html"))

	// Get available indices from database
	availableIndices = getAvailableIndices()
	if len(availableIndices) > 0 {
		currentImageIdx = availableIndices[0] // Start with first available index
	}
}

// startSlideshow starts the automatic slideshow timer
func startSlideshow() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			advanceSlideshowAndBroadcast()
		}
	}()
}

func main() {
	// Start the slideshow timer
	startSlideshow()
	// Start weather updater
	startWeatherUpdater()

	router := http.NewServeMux()

	// Register handlers for HTML templates
	router.HandleFunc("/", withMethod(http.MethodGet, homeHandler))

	// Add helloworld endpoint
	router.HandleFunc("/helloworld", withMethod(http.MethodGet, helloWorldHandler))

	// Add API endpoint for current image data
	// router.HandleFunc("/api/current-image", withMethod(http.MethodGet, getCurrentImageJSON))

	// WebSocket endpoint for slideshow updates
	router.HandleFunc("/ws", withMethod(http.MethodGet, slideshowWebSocketHandler))

	// Serve static files (optional, but good practice for real apps)
	serveStaticFiles(router)

	port := ":8010"
	fmt.Printf("Server starting on port %s\n", port)
	log.Fatal(http.ListenAndServe(port, router))
}
