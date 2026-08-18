.PHONY: test build lint coverage clean

# 默认目标
all: test build

# 运行所有测试
test:
	go test ./... -v

# 运行单元测试
test-unit:
	go test ./internal/... -v

# 运行集成测试
test-integration:
	go test ./test/integration/... -v

# 构建服务
build:
	go build -o bin/user_order_service ./cmd/user_order_service
	go build -o bin/position_monitor_service ./cmd/position_monitor_service
	go build -o bin/exchange_position_reporter ./cmd/exchange_position_reporter

# 代码检查
lint:
	golangci-lint run ./...

# 测试覆盖率
coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# 清理
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# 运行服务
run:
	go run ./cmd/position_monitor_service

run-order:
	go run ./cmd/user_order_service