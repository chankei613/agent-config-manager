.PHONY: build test vet fmt scan tidy clean

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# 実環境スキャン（読み取りのみ。設定ファイルには一切書き込まない）
scan:
	go run ./cmd/acmscan -projects $(HOME)/Library/CloudStorage/Dropbox/_mcp_mini/cometinc

tidy:
	go mod tidy

clean:
	rm -f *.db *.db-shm *.db-wal
