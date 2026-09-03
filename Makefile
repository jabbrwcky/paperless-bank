BINARY  := paperless-bank
CMD     := ./cmd/paperless-bank

.PHONY: build test vet clean generate

build:
	go build -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

# Regenerate the Comdirect API client from comdirect_rest_api_swagger.json.
# See internal/bank/comdirect/generate.go for the go-swagger invocation.
generate:
	go generate ./...

clean:
	rm -f $(BINARY)
