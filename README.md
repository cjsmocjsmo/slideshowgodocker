To run this app you have to have docker installed and running. This app has not been tested on Windows or Mac, however there are windows and mac binars of the rust setup script, start.sh would have to be ported over to what ever windows and mac uses instead of bash. I would be willing to accept PR's from anyone willing to do the work.  This app is mainly targeted to mini pc's (n-100 debian), and the Raspberry Pi 3b+, 4, and 5, start.sh will detect which you are on and use the correct prebuilt rust setup binary. 21,000 jpgs scanned, db populated in 5.6 seconds.  We use golang with templates, my goal is to use the least amount of javascript as possible and keep as much program logic on the backend.

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

Main App Configuration

	- The main app configuration is in start.sh and there are some variables that need to be set for the app to work.
	- DB_PATH is the path to where you want the db to be placed along with it's explicit name. For example "/home/foo/picdb.db"
	- DB_DIR the dir path for the database. For example "/home/foo"
	- IMAGE_DIR is the path to where your images are.
	- IMAGE_BASE_DIR is the dir path to your pics.
	- HTTP_PREFIX will be the prefix to your static http path for example http://10.0.4.55/static/mycoolpic.jpg"
	- PORT is the HTTP port for your server to respond on 
	- TIMEZONE this time zone your are currently in.

Once you have these setting set simple run:

"bash start.sh"

Yes you have to invoke with bash, this script uses "time" which is not available in the normal shell.