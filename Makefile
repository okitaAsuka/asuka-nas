.PHONY: verify
verify:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race -count=1 ./...
	go build ./cmd/asuka-nas
	python3 -B -m unittest discover -s tests -p 'test_preflight.py' -v

.PHONY: verify-ui
verify-ui:
	NAS_BROWSER_TEST=1 go test -race -count=1 ./internal/nas -run '^Test(BrowserProductLoop|IndependentBrowserPreviewOwnership|LibraryCandidateBrowserLoop)$$' -v
