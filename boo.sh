docker build -t slideshowgodocker .
docker run -d \
  --name slideshowgodocker \
  -p 8010:8010 \
  -v /home/piir/go/slideshowgodocker/DB:/app/DB:rw \
  -v /home/pi/Pictures/test2:/app/test2:ro \
  -e TZ=America/Los_Angeles \
  --restart unless-stopped \
  --health-cmd='wget --no-verbose --tries=1 --spider http://localhost:8010/' \
  --health-interval=30s \
  --health-timeout=10s \
  --health-retries=3 \
  --health-start-period=40s \
  slideshowgodocker