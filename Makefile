.PHONY: deploy build clean

SERVER=
DEST_DIR=~/ais_service
MMSI=

deploy:
	rsync -avz --progress -e ssh --exclude=ais_service --exclude=ais_data --exclude=.git . $(SERVER):$(DEST_DIR)
	ssh $(SERVER) "cd $(DEST_DIR) && export PATH=$$PATH:/usr/local/go/bin && make build-linux"

build:
	go mod tidy
	go build -o ais_service *.go

build-linux:
	export CGO_ENABLED=1 GOOS=linux GOARCH=amd64
	go mod tidy
	go build -o ais_service *.go
