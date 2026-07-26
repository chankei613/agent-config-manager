.PHONY: build test vet fmt scan serve app dev ui-test tidy clean

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
	go run ./cmd/acmscan -projects $(HOME)/.claude/mcp_mini/cometinc

tidy:
	go mod tidy

clean:
	rm -f *.db *.db-shm *.db-wal

serve:
	go run ./cmd/acmserve

app:
	$(HOME)/go/bin/wails build

dev:
	$(HOME)/go/bin/wails dev

ui-test:
	cd frontend && npm test
