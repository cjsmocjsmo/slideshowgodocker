DB_PATH=/home/pimedia/go/imagesDB
DB_DIR=/home/pimedia/go
IMAGE_DIR=/home/pimedia/Pictures/MASTERPICS
IMAGE_BASE_DIR=/home/pimedia/Pictures/MASTERPICS
HTTP_PREFIX=/static/
PORT=8010
TIMEZONE="America/Los_Angeles"
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    echo "Running on x86_64 architecture"
    time ./rust/slideshowsetuprust-linux-amd64 \
      --db-path=$DB_PATH \
      --image-dir=$IMAGE_DIR \
      --image-base-dir=$IMAGE_BASE_DIR \
      --http-prefix=$HTTP_PREFIX
elif [ "$ARCH" = "aarch64" ]; then
    echo "Running on aarch64 architecture"
    time ./rust/slideshowsetuprust-linux-arm64 \
      --db-path=$DB_PATH \
      --image-dir=$IMAGE_DIR \
      --image-base-dir=$IMAGE_BASE_DIR \
      --http-prefix=$HTTP_PREFIX 
elif [ "$ARCH" = "armv7l" ]; then
    echo "Running on armv7l architecture"
    time ./rust/slideshowsetuprust-linux-armv7 \
      --db-path=$DB_PATH \
      --image-dir=$IMAGE_DIR \
      --image-base-dir=$IMAGE_BASE_DIR \
      --http-prefix=$HTTP_PREFIX
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

docker build -t slideshowgodocker .
docker run -d \
  --name slideshowgodocker \
  -p $PORT:$PORT \
  -v $DB_DIR:/app/DB:rw \
  -v $IMAGE_DIR:/app/test2:ro \
  -e TZ=$TIMEZONE \
  --restart unless-stopped \
  --health-cmd="wget --no-verbose --tries=1 --spider http://localhost:$PORT/" \
  --health-interval=30s \
  --health-timeout=10s \
  --health-retries=3 \
  --health-start-period=40s \
  slideshowgodocker