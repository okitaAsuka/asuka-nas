.PHONY: verify
verify:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race -count=1 ./...
	go build ./cmd/asuka-nas
	python3 -B -m unittest discover -s tests -p 'test_preflight.py' -v

.PHONY: verify-ui
verify-ui:
	python3 -B tests/proposal_layout_browser.py
	python3 -B tests/naming_assistance_layout_browser.py
	NAS_BROWSER_TEST=1 go test -race -count=1 ./internal/nas -run '^Test(BrowserProductLoop|IndependentBrowserPreviewOwnership|LibraryCandidateBrowserLoop|BatchOrganizerBrowserLoop|RuleProposalBrowserLoop|NamingAssistanceBrowserLoop)$$' -v

# 参数通过环境读取，不拼接到 shell 命令；默认仅本机构建和 save。
export VERSION ARCH PACKAGE_OUT MODULE_CACHE
.PHONY: package-nas package-test
package-nas:
	python3 -B scripts/package_nas.py

package-test:
	python3 -B -m unittest discover -s scripts -p 'test_package_nas.py' -v
