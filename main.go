package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

type ImageData struct {
	Name        string
	Path        string
	Http        string
	Idx         int
	Orientation string
}

// Global variable to store parsed templates
var templates *template.Template
var dbpath = "/app/DB/imagesDB"
var imagedir = "/app/test2/"

// Global variables for slideshow control
var currentImageIdx int = 1
var imageMutex sync.RWMutex
var availableIndices []int

var dbcount = db_count()
var currentSlideIndex = 0 // Index into availableIndices array

func init() {
	// Parse all templates in the "templates" directory.
	// template.Must panics if there's an error, which is good for quick startup
	// errors for templates. In a larger app, you might handle errors more gracefully.
	templates = template.Must(template.ParseGlob("templates/*.html"))

	// Get available indices from database
	availableIndices = get_available_indices()
	if len(availableIndices) > 0 {
		currentImageIdx = availableIndices[0] // Start with first available index
	}
}

func db_count() int {
	db, err := sql.Open("sqlite3", dbpath)
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

func get_available_indices() []int {
	db, err := sql.Open("sqlite3", dbpath)
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

func get_db_image(idx int) (ImageData, error) {
	db, err := sql.Open("sqlite3", dbpath)
	if err != nil {
		log.Printf("Error opening database: %v", err)
		return ImageData{}, err
	}
	defer db.Close()
	// Prepare the query to get image data by index
	var img ImageData
	query := "SELECT name, http, idx, orientation FROM images WHERE idx = ?"
	err = db.QueryRow(query, idx).Scan(&img.Name, &img.Http, &img.Idx, &img.Orientation)
	if err != nil {
		log.Printf("Error querying get_db_image: %v", err)
		return ImageData{}, err
	}
	return img, nil
}

// startSlideshow starts the automatic slideshow timer
func startSlideshow() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			imageMutex.Lock()
			if len(availableIndices) > 0 {
				currentSlideIndex++
				if currentSlideIndex >= len(availableIndices) {
					currentSlideIndex = 0
				}
				currentImageIdx = availableIndices[currentSlideIndex]
				log.Printf("Slideshow advanced to image index %d (position %d of %d)", currentImageIdx, currentSlideIndex+1, len(availableIndices))
			}
			imageMutex.Unlock()
		}
	}()
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	imageMutex.RLock()
	idx := currentImageIdx
	imageMutex.RUnlock()

	fmt.Printf("Available indices: %v, db_count: %d, current_idx: %d, slide_position: %d\n",
		availableIndices, dbcount, idx, currentSlideIndex+1)

	if len(availableIndices) == 0 {
		log.Printf("No images available in database")
		http.Error(w, "No images available", http.StatusInternalServerError)
		return
	}

	data, err1 := get_db_image(idx)
	if err1 != nil {
		log.Printf("Error getting image from database: %v", err1)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	err := templates.ExecuteTemplate(w, "index.html", data)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

// getCurrentImageJSON returns the current image data as JSON
func getCurrentImageJSON(w http.ResponseWriter, r *http.Request) {
	imageMutex.RLock()
	idx := currentImageIdx
	imageMutex.RUnlock()

	if len(availableIndices) == 0 {
		http.Error(w, "No images available", http.StatusInternalServerError)
		return
	}

	data, err := get_db_image(idx)
	if err != nil {
		log.Printf("Error getting image from database: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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

// serveStaticFiles sets up a file server for static assets (like CSS, JS, images).
func serveStaticFiles(router *mux.Router) {
	// Serve static files from /home/pimedia/Pictures/
	staticFileServer := http.FileServer(http.Dir("/app/test2/"))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", staticFileServer))
}

func main() {
	// Start the slideshow timer
	startSlideshow()

	router := mux.NewRouter()

	// Register handlers for HTML templates
	router.HandleFunc("/", homeHandler).Methods("GET")

	// Add helloworld endpoint
	router.HandleFunc("/helloworld", helloWorldHandler).Methods("GET")

	// Add API endpoint for current image data
	router.HandleFunc("/api/current-image", getCurrentImageJSON).Methods("GET")

	// Serve static files (optional, but good practice for real apps)
	// If you have CSS, JS, images, etc., put them in a 'static' folder.
	// You might create a `static` directory like `my-web-app/static/css/style.css`
	serveStaticFiles(router)

	port := ":8010"
	fmt.Printf("Server starting on port %s\n", port)
	log.Fatal(http.ListenAndServe(port, router))
}
