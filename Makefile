build:
	env GOOS=linux GOARCH=amd64	go build -o ./build/app ./server/cmd

chrome:
	rm -rf ./build/chrome
	mkdir -p ./build/chrome
	cp -R ./extension/. ./build/chrome
	mv ./build/chrome/manifest.chrome.json ./build/chrome/manifest.json
	rm -f ./build/chrome/manifest.firefox.json

release:	
	docker buildx build -f ./deploy/Dockerfile --target runner --platform linux/amd64,linux/arm64/v8 -t zoho-mail-checker:latest --push .

default: build

.PHONY: build chrome release default