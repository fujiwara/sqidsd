.PHONY: clean test

sqidsd: go.* *.go
	go build -o $@ ./cmd/sqidsd

clean:
	rm -rf sqidsd dist/

test:
	go test -v ./...

install:
	go install github.com/fujiwara/sqidsd/cmd/sqidsd

dist:
	goreleaser build --snapshot --clean
