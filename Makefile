BINARY  := paperless-bank
CMD     := ./cmd/paperless-bank

.PHONY: build test vet clean

build:
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
