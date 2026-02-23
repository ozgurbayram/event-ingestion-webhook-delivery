.PHONY: build run test test-unit test-integration docker-build docker-up docker-down docker-test clean test-ui

build:
	go build -o bin/server ./cmd/main.go

run:
	go run ./cmd/main.go

test:
	go test -v ./...

test-unit:
	go test -v ./tests/signer_test.go

test-integration:
	docker-compose -f docker-compose.test.yml up --build --abort-on-container-exit
	docker-compose -f docker-compose.test.yml down -v

docker-build:
	docker-compose build

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down -v

docker-logs:
	docker-compose logs -f app

docker-test:
	docker-compose -f docker-compose.test.yml up --build --abort-on-container-exit
	docker-compose -f docker-compose.test.yml down -v

test-ui:
	cd test-ui && python3 -m http.server 3000

clean:
	rm -rf bin/
	docker-compose down -v
	docker-compose -f docker-compose.test.yml down -v

