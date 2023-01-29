### Build
```
docker build -t sfw-sasuke
```
### Run
```
sudo docker run -d \
	--mount type=bind,src="$(pwd)/env",dst=/app/env \
	--mount type=bind,src="$(pwd)/static",dst=/app/static \
	sfw-sasuke
```
