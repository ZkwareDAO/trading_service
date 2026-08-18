# Contributing Guide

This document provides guidelines and instructions for contributing to the Trading Service Risk Control System.

<!-- AUTO-GENERATED: Development Setup -->
## Development Environment Setup

### Prerequisites

- **Go 1.25+**: Install from [golang.org](https://golang.org/dl/)
- **Make**: Build automation (usually pre-installed on Linux/Mac)
- **golangci-lint**: Install with:
  ```bash
  # macOS/Linux
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.54.2
  
  # Or via brew (macOS)
  brew install golangci-lint
  ```

### Initial Setup

```bash
# Clone repository
git clone <repository-url>
cd trading_service

# Install Go dependencies
go mod download

# Verify setup
make test
```

<!-- END AUTO-GENERATED -->

<!-- AUTO-GENERATED: Available Scripts -->
## Available Scripts

| Command | Purpose | When to Use |
|---------|---------|-------------|
| `make all` | Run tests + build | Default; before pushing changes |
| `make test` | Run all tests with verbose output | Full validation; note some external integration tests may require network/API availability |
| `make test-unit` | Run `go test ./internal/... -v` | Quick internal package validation |
| `make test-integration` | Run `go test ./test/integration/... -v` | Integration suite if present |
| `make build` | Compile `bin/user_order_service`, `bin/position_monitor_service`, and `bin/exchange_position_reporter` | Prepare binaries for deploy/run |
| `make lint` | Run `golangci-lint run ./...` | Before committing when golangci-lint is installed |
| `make coverage` | Generate `coverage.out` and `coverage.html` | Coverage review |
| `make clean` | Remove `bin/` and coverage artifacts | Clean workspace |
| `make run` | Run `position_monitor_service` via `go run` | Local PMS-only development |
| `make run-order` | Run `user_order_service` via `go run` | Local UOS-only development |
| `./run_test.sh` | Build and run both services in foreground-style test mode | Manual local test flow |
| `./deploy.sh` | Build, initialize missing CSV headers, and start both services with `nohup` logs | Local/remote deployment |

<!-- END AUTO-GENERATED -->

## Testing Procedures

### Test Requirements

- **Minimum Coverage**: 80% required for all packages
- **Test Types**: Unit tests, integration tests required
- **Test Structure**: Follow AAA pattern (Arrange-Act-Assert)

### Running Tests

```bash
# Run all tests with verbose output
make test

# Run specific package
go test ./internal/risk/engine/... -v

# Run with coverage
make coverage
open coverage.html

# Check current coverage
go test ./... -cover
```

### Writing Tests

**Test File Location**: Place test files in the same directory as the code they test.

**Naming Convention**: Use `<filename>_test.go` format.

**Test Structure Example**:

```go
func TestRuleEngine_EvaluateCondition(t *testing.T) {
    // Arrange
    ctx := &RiskContext{
        Local: LocalMetrics{ROI: -0.08},
    }
    condition := &Condition{
        Field:    "ROI",
        Operator: "<",
        Value:    -0.05,
    }
    engine := NewRuleEngine()

    // Act
    result := engine.EvaluateCondition(ctx, condition)

    // Assert
    assert.True(t, result, "ROI -0.08 should trigger condition ROI < -0.05")
}
```

**Test Naming**: Use descriptive names explaining behavior:
- `TestFunctionName_ScenarioDescription`
- Example: `TestCalculateROI_LongPositionWithProfit`

### Test Coverage Standards

Each package should maintain:
- **Unit Tests**: Test individual functions/methods
- **Integration Tests**: Test component interactions
- **Edge Cases**: Test boundary conditions and error scenarios

**Coverage by Package** (current as of 2026-07-31):
- `risk/signal`: 100.0%
- `risk/metrics`: 95.5%
- `risk/scheduler`: 91.7%
- `risk/state`: 86.1%
- `config`: 83.1%
- `reporter`: 85.4%
- `exchange/deribit`: 80.9%
- `api`: 79.1%
- `risk/engine`: 74.3%
- `exchange`: 70.3%
- `risk/executor`: 68.5%
- `deribit_position_sync`: 63.5%
- `notification`: 63.9%
- `persistence`: 65.1%
- `risk/aggregator`: 64.7%
- `risk/pipeline`: 57.6%
- `exchange/ws`: 58.7% (Deribit WS: heartbeat + reconnect tested)
- `rpc`: 61.7%
- `position_monitor_service`: 79.1%
- `risk/config`: 55.6%
- `exchange/hyperliquid`: 69.5%
- `signal`: 62.4% (some tests failing due to credential requirements)
- `exchange/binance`: 25.8%
- `order`: 16.5%
- `user_order_service`: 5.0%

## Code Style Enforcement

### Linting

Run before every commit:
```bash
make lint
```

### Formatting

```bash
# Format all code
go fmt ./...

# Or use goimports for import ordering
goimports -w .
```

### Code Standards

Follow these principles:

1. **Immutability**: Never mutate existing structs; return new copies
2. **KISS**: Keep functions simple and focused
3. **DRY**: Extract repeated logic into utilities
4. **YAGNI**: Don't add features before they're needed
5. **File Organization**: Files should be < 800 lines, functions < 50 lines
6. **Error Handling**: Handle all errors explicitly
7. **Naming**: Use descriptive names (camelCase for vars, PascalCase for types)
8. **Constants Over Magic Strings**: Use named constants instead of hardcoded strings
   - ✅ `if sig.Exchange == ExchangeDeribit`
   - ❌ `if sig.Exchange == "deribit"`
   - Benefits: prevents typos, IDE autocomplete, centralized management

### Pre-commit Checks

Before committing:
```bash
# 1. Format code
go fmt ./...

# 2. Run linter
make lint

# 3. Run tests
make test

# 4. Check coverage
make coverage

# 5. Verify coverage >= 80%
go test ./... -cover
```

## Git Workflow

### Branch Naming

Use descriptive branch names:
- `feat/add-stop-loss-action` - New feature
- `fix/roi-calculation-bug` - Bug fix
- `refactor/improve-state-management` - Refactoring

### Commit Message Format

```
<type>: <description>

[optional body]
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code restructuring
- `docs`: Documentation updates
- `test`: Test additions/updates
- `chore`: Build/tooling changes
- `perf`: Performance improvements

**Examples**:
```
feat: add hedge position action executor

fix: correct max drawdown calculation for short positions

refactor: extract position aggregation to dedicated package
```

### Pull Request Process

1. **Before PR**:
   ```bash
   # Ensure all checks pass
   make all
   make lint
   
   # Check coverage
   go test ./... -cover
   ```

2. **PR Requirements**:
   - All tests passing
   - Coverage >= 80%
   - No linting errors
   - Documentation updated (if applicable)
   - Meaningful PR description

3. **PR Description Template**:
   ```markdown
   ## Changes
   [Describe what changed and why]
   
   ## Testing
   [How tested + test coverage]
   
   ## Checklist
   - [ ] Tests pass
   - [ ] Coverage >= 80%
   - [ ] Lint clean
   - [ ] Docs updated
   ```

## Code Review Standards

### Review Checklist

Before requesting review, ensure:
- [ ] Code is readable and well-named
- [ ] Functions are focused (<50 lines)
- [ ] Files are cohesive (<800 lines)
- [ ] No deep nesting (>4 levels)
- [ ] Errors handled explicitly
- [ ] No hardcoded secrets/credentials
- [ ] No debug statements (console.log)
- [ ] Tests exist for new functionality
- [ ] Coverage meets 80% minimum

### Security Checklist

For code handling:
- User input validation
- Database queries
- File system operations
- External API calls
- Authentication/authorization
- Cryptographic operations

**STOP**: Use security-reviewer agent and address all CRITICAL issues before proceeding.

## Architecture Guidelines

### Package Organization

Organize by domain/feature, not by type:
```
internal/risk/
├── engine/      # Rule evaluation (domain)
├── metrics/     # Financial calculations (domain)
├── executor/    # Action execution (domain)
```

### Dependency Rules

- Packages can depend on sibling packages
- Packages can depend on parent packages
- Packages should NOT depend on child packages
- Avoid circular dependencies

### Design Patterns Used

1. **Repository Pattern**: For data access (CSV files)
2. **Pipeline Pattern**: For risk processing flow
3. **Chain of Responsibility**: For rule evaluation
4. **Strategy Pattern**: For action execution
5. **RPC Pattern**: For cross-service communication (策略管理统一到 UOS)

<!-- AUTO-GENERATED: Package Structure -->
### Risk Package Structure

```
internal/risk/
├── aggregator/    # Position aggregation (user_order_positions → user_positions)
├── config/        # Rule configuration (runtime RuleStore)
├── engine/        # Rule evaluation (RuleEngine, IsValidPositionSymbol, NormalizePositionSymbol, evaluatePositionCondition)
├── executor/      # Action execution (ActionResult, RiskActionApplier)
├── metrics/       # Local metrics (PnL, ROI, LocalMetrics)
├── pipeline/      # Pipeline orchestration (RiskPipeline.Run)
├── scheduler/     # SyncLoop & pipeline scheduling
├── signal/        # Risk signal types
├── state/         # Atomic GlobalState management
├── types.go       # Core types (UserPosition, RiskContext, Rule)
```

### Internal Packages

```
internal/
├── api/           # HTTP endpoints (signal receiver, rules API)
├── config/        # YAML/env config (PMExchangeConfig, etc.)
├── exchange/      # Exchange interface + mock executor
│   ├── binance/   # Binance Futures adapter
│   ├── hyperliquid/# Hyperliquid DEX adapter
│   ├── deribit/   # Deribit Options adapter (NEW: WebSocket price manager)
│   └── ws/        # WebSocket connection management (Deribit order monitor + notifications)
├── notification/  # Alert/notification handling (Open/Close/Test/DeribitPosition/ManualClose channels; ManualClose carries current ROI)
├── order/         # Order models (UserOrder, UserStrategy, etc.)
├── persistence/   # CSV engine, GlobalState, StateRepository
├── rpc/           # HTTP JSON RPC client/server
├── risk/          # Risk control core (engine, pipeline, aggregator, etc.)
├── signal/        # Signal handler (HandleOpen/Close/Reverse + nested signals)
```

## Documentation Standards

### When to Update Docs

- New features → Update README.md
- API changes → Update API documentation
- Configuration changes → Update README.md Configuration section
- Test changes → Update coverage report

### Doc Formatting

- Use markdown formatting
- Include code examples
- Keep sections organized
- Update table of contents if needed

## Getting Help

- Check existing documentation
- Review test examples for usage patterns
- Ask in team channels
- Reference the plan document for architecture decisions

## Quick Reference

### Current Test Coverage

| Package | Coverage | Status |
|---------|----------|--------|
| risk/signal | 100.0% | ✅ Excellent |
| risk/metrics | 95.5% | ✅ Excellent |
| risk/scheduler | 91.7% | ✅ Excellent |
| risk/state | 86.1% | ✅ Excellent |
| reporter | 85.4% | ✅ Excellent |
| config | 83.1% | ✅ Excellent |
| exchange/deribit | 80.9% | ✅ Good |
| api | 79.1% | ✅ Good |
| risk/engine | 74.3% | ✅ Good |
| exchange | 70.3% | ⚠️ Needs more tests |
| risk/executor | 68.5% | ⚠️ Needs more tests |
| exchange/hyperliquid | 69.5% | ⚠️ Needs more tests |
| notification | 63.9% | ⚠️ Needs more tests |
| persistence | 65.1% | ⚠️ Needs more tests |
| deribit_position_sync | 63.5% | ⚠️ Needs more tests |
| risk/aggregator | 64.7% | ⚠️ Needs more tests |
| rpc | 61.7% | ⚠️ Needs more tests |
| signal | 62.4% | ⚠️ Needs more tests |
| exchange/ws | 58.7% | ⚠️ Needs more tests |
| risk/pipeline | 57.6% | ⚠️ Needs more tests |
| risk/config | 55.6% | ⚠️ Needs more tests |
| exchange/binance | 25.8% | ⚠️ Needs more tests |
| order | 16.5% | ⚠️ Needs more tests |
| user_order_service | 5.0% | ⚠️ Needs more tests |

### File Size Guidelines

- **Functions**: < 50 lines (focused, single responsibility)
- **Files**: < 800 lines (cohesive, organized by feature)
- **Packages**: Logical grouping of related functionality

### Common Commands

```bash
# Quick validation
make test-unit

# Full validation
make all

# Coverage check
make coverage

# Local run
make run

# Clean start
make clean && make all
```