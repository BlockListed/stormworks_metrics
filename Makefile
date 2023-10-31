generate-css:
	@echo "Generating tailwind css code"
	@npm run build

generate-templ:
	@echo "Generating templ template go code"
	@templ generate

build: generate-templ generate-css
	@echo "Building application"
	@go build

run: build
	@./stormworks-metrics
