.PHONY: build clean web test-touch

build:
	@mkdir -p build
	CGO_ENABLED=1 go build -ldflags="-s -w" -o build/r1-toolbox ./cmd

test-touch:
	@mkdir -p build
	@echo "构建触摸屏测试程序..."
	CGO_ENABLED=0 go build -ldflags="-s -w" -o build/test-touch ./cmd/test_touch
	@echo "✓ 测试程序已构建到 build/test-touch"

web:
	@echo "构建 Web UI..."
	@cd web-ui && npm install && npm run build
	@echo "✓ Web UI 已构建到 internal/web/dist/"

clean:
	@rm -rf build
	@rm -rf internal/web/dist
