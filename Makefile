.PHONY: build clean test vet check

build:
	go build -o mcp-gcal .

test:
	go test ./...

vet:
	go vet ./...

check: test vet build

clean:
	rm -f mcp-gcal
