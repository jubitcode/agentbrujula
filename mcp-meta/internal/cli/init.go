package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitCommand scaffolds a new tool project.
type InitCommand struct{}

func (c *InitCommand) Name() string        { return "init" }
func (c *InitCommand) Description() string { return "Create a new tool project" }

func (c *InitCommand) Usage() string {
	return `mcp-meta init - Create a new tool project

USAGE:
    mcp-meta init <name> [options]

OPTIONS:
    --language <LANG>    Language: python, go, node (default: python)
    --namespace <NS>     Publisher namespace (default: local)
    --dir <DIR>          Output directory (default: ./<name>)
    -h, --help           Print help

EXAMPLES:
    mcp-meta init my-tool
    mcp-meta init my-tool --language go
    mcp-meta init my-tool --namespace myorg --language python
`
}

func (c *InitCommand) Run(args []string) error {
	// First check for help before other parsing
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			fmt.Print(c.Usage())
			return nil
		}
	}

	// Get project name from first positional arg (before flags)
	var name string
	var flagArgs []string

	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flagArgs = args[i:]
			break
		}
		if name == "" {
			name = arg
		} else {
			flagArgs = args[i:]
			break
		}
	}
	if name == "" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		if len(args) > 1 {
			flagArgs = args[1:]
		}
	}

	fs := flag.NewFlagSet("init", flag.ContinueOnError)

	var language string
	var namespace string
	var dir string

	fs.StringVar(&language, "language", "python", "Language: python, go, node")
	fs.StringVar(&namespace, "namespace", "local", "Publisher namespace")
	fs.StringVar(&dir, "dir", "", "Output directory")

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	// Get project name from positional args if not already set
	if name == "" {
		if fs.NArg() > 0 {
			name = fs.Arg(0)
		}
	}

	if name == "" {
		return fmt.Errorf("project name is required\n\nUsage: mcp-meta init <name> [options]")
	}

	// Validate name
	if !isValidName(name) {
		return fmt.Errorf("invalid project name: %q (must be lowercase alphanumeric with hyphens)", name)
	}

	// Validate language
	switch language {
	case "python", "go", "node":
		// Valid
	default:
		return fmt.Errorf("invalid language: %q (must be python, go, or node)", language)
	}

	// Set output directory
	if dir == "" {
		dir = name
	}

	// Check if directory exists
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("directory already exists: %s", dir)
	}

	// Create project
	fmt.Printf("Creating new %s tool project: %s\n", language, name)

	if err := createProject(dir, name, namespace, language); err != nil {
		// Clean up on error
		os.RemoveAll(dir)
		return fmt.Errorf("failed to create project: %w", err)
	}

	fmt.Printf("\nProject created successfully!\n\n")
	fmt.Printf("Next steps:\n")
	fmt.Printf("  cd %s\n", dir)
	fmt.Printf("  mcp-meta dev        # Start development server\n")
	fmt.Printf("  mcp-meta test       # Run tests\n")
	fmt.Printf("  mcp-meta build      # Build container image\n")
	fmt.Printf("  mcp-meta publish    # Publish to marketplace\n")

	return nil
}

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func createProject(dir, name, namespace, language string) error {
	// Create directories
	dirs := []string{
		dir,
		filepath.Join(dir, "tests"),
	}

	for _, d := range dirs {
		if err := ensureDir(d); err != nil {
			return err
		}
	}

	// Create manifest
	manifest := generateManifest(name, namespace, language)
	if err := writeFile(filepath.Join(dir, "mcp-meta.yaml"), manifest); err != nil {
		return err
	}

	// Create language-specific files
	switch language {
	case "python":
		return createPythonProject(dir, name)
	case "go":
		return createGoProject(dir, name)
	case "node":
		return createNodeProject(dir, name)
	}

	return nil
}

func generateManifest(name, namespace, language string) string {
	runtime := map[string]string{
		"python": "python:3.12",
		"go":     "go:1.22",
		"node":   "node:20",
	}[language]

	command := map[string]string{
		"python": `["uv", "run", "python", "main.py"]`,
		"go":     `["go", "run", "main.go"]`,
		"node":   `["node", "index.js"]`,
	}[language]

	return fmt.Sprintf(`# MCP-Meta Tool Manifest
# See https://mcp-tools.io/docs/manifest for full documentation

apiVersion: mcp-meta/v1
kind: ToolPod

metadata:
  name: %s
  namespace: %s
  version: "0.1.0"
  description: "A new MCP tool"
  license: "MIT"
  keywords:
    - example
  labels:
    category: utilities
    language: %s

spec:
  build:
    runtime: %s

  command: %s
  workdir: .

  resources:
    cpu: "0.5"
    memory: "256Mi"
    timeout: 60s

  startupTimeout: 10s
  callTimeout: 30s
  shutdownTimeout: 5s

  healthCheck:
    enabled: true
    interval: 30s

  tools:
    - name: hello
      description: "A simple greeting tool"
      timeout: 10s
      inputSchema:
        type: object
        required:
          - name
        properties:
          name:
            type: string
            description: "Name to greet"
        additionalProperties: false

marketplace:
  pricing:
    model: free
  visibility: public
  categories:
    - utilities
`, name, namespace, language, runtime, command)
}

func createPythonProject(dir, name string) error {
	// main.py
	main := `#!/usr/bin/env python3
"""MCP Tool: ` + name + `

A JSON-RPC 2.0 server that processes tool calls from mcp-meta.
"""
import sys
import json
from typing import Any


def handle_hello(params: dict) -> dict:
    """Handle the hello tool call."""
    name = params.get("name", "World")
    return {"message": f"Hello, {name}!"}


def handle_ping(params: dict) -> dict:
    """Health check."""
    return {"status": "ok"}


def handle_shutdown(params: dict) -> dict:
    """Graceful shutdown."""
    response = {"status": "shutting_down"}
    print(json.dumps({"jsonrpc": "2.0", "id": None, "result": response}))
    sys.exit(0)


# Tool registry
TOOLS = {
    "hello": handle_hello,
    "ping": handle_ping,
    "shutdown": handle_shutdown,
}


def handle_request(request: dict) -> dict:
    """Process a JSON-RPC request."""
    method = request.get("method")
    params = request.get("params", {})
    request_id = request.get("id")

    if method not in TOOLS:
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32601, "message": f"Method not found: {method}"}
        }

    try:
        result = TOOLS[method](params)
        return {"jsonrpc": "2.0", "id": request_id, "result": result}
    except Exception as e:
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {"code": -32000, "message": str(e)}
        }


def main():
    """Main entry point - reads JSON-RPC requests from stdin."""
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            request = json.loads(line)
            response = handle_request(request)
            print(json.dumps(response))
            sys.stdout.flush()
        except json.JSONDecodeError as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": None,
                "error": {"code": -32700, "message": f"Parse error: {e}"}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()


if __name__ == "__main__":
    main()
`

	// pyproject.toml
	pyproject := fmt.Sprintf(`[project]
name = "%s"
version = "0.1.0"
description = "An MCP tool"
requires-python = ">=3.10"
dependencies = []

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.uv]
dev-dependencies = [
    "pytest>=8.0.0",
]
`, name)

	// .gitignore
	gitignore := `__pycache__/
*.py[cod]
*$py.class
.venv/
.uv/
dist/
build/
*.egg-info/
.pytest_cache/
`

	// README.md
	readme := fmt.Sprintf(`# %s

An MCP tool built with mcp-meta.

## Development

`+"```"+`bash
# Install dependencies
uv sync

# Run development server
mcp-meta dev

# Run tests
mcp-meta test
`+"```"+`

## Usage

`+"```"+`bash
# Build container
mcp-meta build

# Publish to marketplace
mcp-meta publish
`+"```"+`
`, name)

	files := map[string]string{
		"main.py":        main,
		"pyproject.toml": pyproject,
		".gitignore":     gitignore,
		"README.md":      readme,
	}

	for filename, content := range files {
		if err := writeFile(filepath.Join(dir, filename), content); err != nil {
			return err
		}
	}

	// Create test file
	testContent := `"""Tests for the tool."""
import json
from main import handle_request


def test_hello():
    request = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "hello",
        "params": {"name": "Test"}
    }
    response = handle_request(request)
    assert response["result"]["message"] == "Hello, Test!"


def test_ping():
    request = {
        "jsonrpc": "2.0",
        "id": 2,
        "method": "ping",
        "params": {}
    }
    response = handle_request(request)
    assert response["result"]["status"] == "ok"
`
	return writeFile(filepath.Join(dir, "tests", "test_tool.py"), testContent)
}

func createGoProject(dir, name string) error {
	modName := strings.ReplaceAll(name, "-", "")

	// main.go
	main := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type Request struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      any             ` + "`json:\"id\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params\"`" + `
}

type Response struct {
	JSONRPC string      ` + "`json:\"jsonrpc\"`" + `
	ID      any         ` + "`json:\"id\"`" + `
	Result  interface{} ` + "`json:\"result,omitempty\"`" + `
	Error   *Error      ` + "`json:\"error,omitempty\"`" + `
}

type Error struct {
	Code    int    ` + "`json:\"code\"`" + `
	Message string ` + "`json:\"message\"`" + `
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			sendError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		resp := handleRequest(req)
		sendResponse(resp)
	}
}

func handleRequest(req Request) Response {
	switch req.Method {
	case "hello":
		return handleHello(req)
	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]string{"status": "ok"}}
	case "shutdown":
		resp := Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]string{"status": "shutting_down"}}
		sendResponse(resp)
		os.Exit(0)
		return resp
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32601, Message: "Method not found: " + req.Method},
		}
	}
}

func handleHello(req Request) Response {
	var params struct {
		Name string ` + "`json:\"name\"`" + `
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &Error{Code: -32602, Message: err.Error()}}
	}

	if params.Name == "" {
		params.Name = "World"
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]string{"message": fmt.Sprintf("Hello, %s!", params.Name)},
	}
}

func sendResponse(resp Response) {
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

func sendError(id any, code int, message string) {
	resp := Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
	sendResponse(resp)
}
`

	// go.mod
	gomod := fmt.Sprintf(`module %s

go 1.22
`, modName)

	// .gitignore
	gitignore := `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary
*.test

# Output
/bin/
/dist/

# IDE
.idea/
.vscode/
`

	// README.md
	readme := fmt.Sprintf(`# %s

An MCP tool built with mcp-meta.

## Development

`+"```"+`bash
# Run development server
mcp-meta dev

# Run tests
go test ./...
`+"```"+`

## Usage

`+"```"+`bash
# Build container
mcp-meta build

# Publish to marketplace
mcp-meta publish
`+"```"+`
`, name)

	files := map[string]string{
		"main.go":    main,
		"go.mod":     gomod,
		".gitignore": gitignore,
		"README.md":  readme,
	}

	for filename, content := range files {
		if err := writeFile(filepath.Join(dir, filename), content); err != nil {
			return err
		}
	}

	return nil
}

func createNodeProject(dir, name string) error {
	// index.js
	main := `#!/usr/bin/env node
/**
 * MCP Tool: ` + name + `
 *
 * A JSON-RPC 2.0 server that processes tool calls from mcp-meta.
 */

const readline = require('readline');

const tools = {
  hello: (params) => {
    const name = params.name || 'World';
    return { message: ` + "`Hello, ${name}!`" + ` };
  },

  ping: () => {
    return { status: 'ok' };
  },

  shutdown: () => {
    const response = { status: 'shutting_down' };
    console.log(JSON.stringify({ jsonrpc: '2.0', id: null, result: response }));
    process.exit(0);
  }
};

function handleRequest(request) {
  const { method, params = {}, id } = request;

  if (!tools[method]) {
    return {
      jsonrpc: '2.0',
      id,
      error: { code: -32601, message: ` + "`Method not found: ${method}`" + ` }
    };
  }

  try {
    const result = tools[method](params);
    return { jsonrpc: '2.0', id, result };
  } catch (error) {
    return {
      jsonrpc: '2.0',
      id,
      error: { code: -32000, message: error.message }
    };
  }
}

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

rl.on('line', (line) => {
  if (!line.trim()) return;

  try {
    const request = JSON.parse(line);
    const response = handleRequest(request);
    console.log(JSON.stringify(response));
  } catch (error) {
    console.log(JSON.stringify({
      jsonrpc: '2.0',
      id: null,
      error: { code: -32700, message: ` + "`Parse error: ${error.message}`" + ` }
    }));
  }
});
`

	// package.json
	packageJSON := fmt.Sprintf(`{
  "name": "%s",
  "version": "0.1.0",
  "description": "An MCP tool",
  "main": "index.js",
  "scripts": {
    "start": "node index.js",
    "test": "node --test tests/"
  },
  "keywords": ["mcp", "tool"],
  "license": "MIT"
}
`, name)

	// .gitignore
	gitignore := `node_modules/
dist/
*.log
.DS_Store
`

	// README.md
	readme := fmt.Sprintf(`# %s

An MCP tool built with mcp-meta.

## Development

`+"```"+`bash
# Install dependencies
npm install

# Run development server
mcp-meta dev

# Run tests
npm test
`+"```"+`

## Usage

`+"```"+`bash
# Build container
mcp-meta build

# Publish to marketplace
mcp-meta publish
`+"```"+`
`, name)

	files := map[string]string{
		"index.js":     main,
		"package.json": packageJSON,
		".gitignore":   gitignore,
		"README.md":    readme,
	}

	for filename, content := range files {
		if err := writeFile(filepath.Join(dir, filename), content); err != nil {
			return err
		}
	}

	return nil
}
