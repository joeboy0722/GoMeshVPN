APPSOURCE = main.go
APPNAME = GoMeshVPN

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build-linux: ## Build for Linux (using wails)
	wails build -platform linux/amd64

build-windows: ## Build for Windows (using wails)
	wails build -platform windows/amd64

build-go: ## Build using standard go build (requires frontend/dist to be present)
	go build -o $(APPNAME) $(APPSOURCE)

deps: ## Download dependencies
	go mod tidy

clean: ## Remove build artifacts
	rm -f $(APPNAME)
	rm -f $(APPNAME).exe
