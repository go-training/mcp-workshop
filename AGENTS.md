# Repository Agent Coding Guidelines

## Build/Test/Lint Commands

- **Build server:** `go build -o mcp-server server.go` (per module)
- **Run all tests:** `go test ./...`
- **Run single test:** `go test -run TestFuncName ./path/to/your/package`
- **Recommend lint:** `go vet ./...` (use `golangci-lint run` if configured via .golangci.yml)

## Code Style Guidelines

- **Imports:** Group stdlib, then third-party, then internal; use goimports or `gofmt -s`.
- **Formatting:** Enforce gofmt; use tabs for indentation, max line length 120.
- **Types:** Use clear, descriptive type names. Exported types/functions use CamelCase.
- **Naming:** Functions/vars: `camelCase` for locals/receivers, `CamelCase` for exports. Use `ID`, `URL`, etc.
- **Error handling:** Always check errors. Prefer `fmt.Errorf` with context, never ignore errors.
- **Logging:** Use `slog`/`pkg/logger` for all logs; production uses JSON/info, development uses text/debug.
- **Testing:** Place `_test.go` beside sources. Use table-driven tests for complex logic.
- **Modules:** Each major folder is a standalone Go module/binary if it has server.go.
- **Files:** Each Go file should start with `package` and doc comment for exported packages/classes.
- **Security:** Mask tokens in logs (`show_auth_token` does this). Never print or log full secrets.
- **Observability:** Use OpenTelemetry for tracing where applicable; inject context info.

No Copilot or Cursor rules detected. Add new guidelines here as needed.
