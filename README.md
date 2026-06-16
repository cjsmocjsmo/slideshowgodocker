Run python3 setup.py

then

docker-compose up --build -d

docker-compose logs slideshow

docker-compose down

Weather configuration

- Configure weather location in config/weather.json:

{
	"location": "Belfair, WA",
	"lat": 47.4515,
	"lon": -122.8276
}

- NOAA weather requests use the configured lat/lon when both are provided.
- If lat/lon are omitted, the app geocodes the location string.
- If config/weather.json is missing, empty, invalid, or location is blank, the app falls back to Walla Walla, WA.
- On startup, the app logs the effective weather location and coordinates in use.

Music station configuration

- Configure one station in config/music.json:

{
	"name": "HANK FM Seattle",
	"sources": [
		{
			"src": "https://playerservices.streamtheworld.com/api/livestream-redirect/KPLZFM.mp3",
			"type": "audio/mpeg"
		}
	]
}

- Update this file whenever you want to change the station.
- There are no UI controls for station changes.
- If config/music.json is missing, empty, invalid, or has no valid sources, the app falls back to a built-in default station.