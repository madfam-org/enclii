package builder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
)

// FunctionRuntime represents a supported function runtime
type FunctionRuntime string

const (
	RuntimeGo     FunctionRuntime = "go"
	RuntimePython FunctionRuntime = "python"
	RuntimeNode   FunctionRuntime = "node"
	RuntimeRust   FunctionRuntime = "rust"
)

// FunctionBuildConfig contains detected function configuration
type FunctionBuildConfig struct {
	Runtime      FunctionRuntime
	Handler      string
	EntryFile    string
	FunctionsDir string
}

// IsFunctionBuild checks if the build directory contains a functions/ directory
func IsFunctionBuild(buildDir string) bool {
	functionsDir := filepath.Join(buildDir, "functions")
	info, err := os.Stat(functionsDir)
	return err == nil && info.IsDir()
}

// DetectFunctionRuntime detects the function runtime based on files in functions/
func DetectFunctionRuntime(buildDir string) (*FunctionBuildConfig, error) {
	functionsDir := filepath.Join(buildDir, "functions")

	// Check for Go (go.mod or main.go)
	if _, err := os.Stat(filepath.Join(functionsDir, "go.mod")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimeGo,
			Handler:      "main.Handler",
			EntryFile:    "main.go",
			FunctionsDir: functionsDir,
		}, nil
	}
	if _, err := os.Stat(filepath.Join(functionsDir, "main.go")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimeGo,
			Handler:      "main.Handler",
			EntryFile:    "main.go",
			FunctionsDir: functionsDir,
		}, nil
	}

	// Check for Python (requirements.txt or handler.py)
	if _, err := os.Stat(filepath.Join(functionsDir, "requirements.txt")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimePython,
			Handler:      "handler.main",
			EntryFile:    "handler.py",
			FunctionsDir: functionsDir,
		}, nil
	}
	if _, err := os.Stat(filepath.Join(functionsDir, "handler.py")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimePython,
			Handler:      "handler.main",
			EntryFile:    "handler.py",
			FunctionsDir: functionsDir,
		}, nil
	}

	// Check for Node.js (package.json or handler.js/handler.ts)
	if _, err := os.Stat(filepath.Join(functionsDir, "package.json")); err == nil {
		// Check if TypeScript
		entryFile := "handler.js"
		if _, err := os.Stat(filepath.Join(functionsDir, "handler.ts")); err == nil {
			entryFile = "handler.ts"
		}
		return &FunctionBuildConfig{
			Runtime:      RuntimeNode,
			Handler:      "handler.main",
			EntryFile:    entryFile,
			FunctionsDir: functionsDir,
		}, nil
	}
	if _, err := os.Stat(filepath.Join(functionsDir, "handler.js")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimeNode,
			Handler:      "handler.main",
			EntryFile:    "handler.js",
			FunctionsDir: functionsDir,
		}, nil
	}

	// Check for Rust (Cargo.toml)
	if _, err := os.Stat(filepath.Join(functionsDir, "Cargo.toml")); err == nil {
		return &FunctionBuildConfig{
			Runtime:      RuntimeRust,
			Handler:      "handler",
			EntryFile:    "src/main.rs",
			FunctionsDir: functionsDir,
		}, nil
	}

	return nil, fmt.Errorf("no recognized function runtime in %s", functionsDir)
}

// GetFunctionDockerfile returns the Dockerfile content for a given runtime
func GetFunctionDockerfile(runtime FunctionRuntime) string {
	switch runtime {
	case RuntimeGo:
		return goDockerfile
	case RuntimePython:
		return pythonDockerfile
	case RuntimeNode:
		return nodeDockerfile
	case RuntimeRust:
		return rustDockerfile
	default:
		return ""
	}
}

// Dockerfile templates optimized for cold start performance

const goDockerfile = `# Enclii Function - Go Runtime
# Target cold start: <500ms

# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first for caching
COPY functions/go.mod functions/go.sum* ./
RUN go mod download || true

# Copy source
COPY functions/ .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -o /function .

# Runtime stage - minimal distroless image
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /function /function

# Function runs on port 8080
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/function"]
`

const pythonDockerfile = `# Enclii Function - Python Runtime
# Target cold start: <3s

FROM python:3.12-slim AS builder

WORKDIR /app

# Install dependencies
COPY functions/requirements.txt ./
RUN pip install --no-cache-dir --target=/app/deps -r requirements.txt

# Runtime stage
FROM python:3.12-slim

WORKDIR /app

# Copy dependencies
COPY --from=builder /app/deps /app/deps

# Copy function code
COPY functions/ .

# Set Python path
ENV PYTHONPATH=/app/deps:/app
ENV PYTHONUNBUFFERED=1

# Function runs on port 8080
EXPOSE 8080

# Use non-root user
RUN useradd -m -u 1000 function
USER function

# Default handler - expects handler.py with main() function
CMD ["python", "-m", "uvicorn", "handler:app", "--host", "0.0.0.0", "--port", "8080"]
`

const nodeDockerfile = `# Enclii Function - Node.js Runtime
# Target cold start: <2s

FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY functions/package*.json ./

# Install production dependencies only
RUN npm ci --omit=dev --ignore-scripts

# Copy source
COPY functions/ .

# Build TypeScript if present
RUN if [ -f "tsconfig.json" ]; then npm run build 2>/dev/null || true; fi

# Runtime stage
FROM node:20-alpine

WORKDIR /app

# Copy from builder
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app .

# Function runs on port 8080
EXPOSE 8080

# Use non-root user
USER node

# Default handler
CMD ["node", "handler.js"]
`

const rustDockerfile = `# Enclii Function - Rust Runtime
# Target cold start: <500ms

# Build stage
FROM rust:1.75-alpine AS builder

RUN apk add --no-cache musl-dev

WORKDIR /app

# Copy Cargo files for caching
COPY functions/Cargo.toml functions/Cargo.lock* ./

# Create dummy src to cache dependencies
RUN mkdir src && echo 'fn main() {}' > src/main.rs
RUN cargo build --release && rm -rf src

# Copy actual source
COPY functions/src ./src

# Build release binary with musl for static linking
RUN cargo build --release --target x86_64-unknown-linux-musl || cargo build --release

# Runtime stage - minimal distroless image
FROM gcr.io/distroless/cc-debian12:nonroot

# Copy binary (try musl target first, then default)
COPY --from=builder /app/target/x86_64-unknown-linux-musl/release/function /function 2>/dev/null || true
COPY --from=builder /app/target/release/function /function 2>/dev/null || true

# Function runs on port 8080
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/function"]
`

// buildFunction builds a function image using the appropriate runtime Dockerfile
func (e *Executor) buildFunction(ctx context.Context, job *queue.BuildJob, buildDir string) (string, error) {
	// Detect runtime
	fnConfig, err := DetectFunctionRuntime(buildDir)
	if err != nil {
		return "", fmt.Errorf("function detection failed: %w", err)
	}

	e.log(job.ID, "🚀 Function detected: runtime=%s, handler=%s", fnConfig.Runtime, fnConfig.Handler)

	// Get Dockerfile content for runtime
	dockerfileContent := GetFunctionDockerfile(fnConfig.Runtime)
	if dockerfileContent == "" {
		return "", fmt.Errorf("unsupported function runtime: %s", fnConfig.Runtime)
	}

	// Write Dockerfile to build directory
	dockerfilePath := filepath.Join(buildDir, "Dockerfile.function")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write function Dockerfile: %w", err)
	}

	// Generate image tag (use function-specific path)
	imageTag := e.generateFunctionImageTag(job)

	e.log(job.ID, "🐳 Building function with %s runtime...", fnConfig.Runtime)

	// Build arguments
	args := []string{
		"build",
		"-t", imageTag,
		"-f", "Dockerfile.function",
	}

	// Add labels
	args = append(args,
		"--label", fmt.Sprintf("org.opencontainers.image.revision=%s", job.GitSHA),
		"--label", fmt.Sprintf("org.opencontainers.image.source=%s", job.GitRepo),
		"--label", "io.enclii.function=true",
		"--label", fmt.Sprintf("io.enclii.function.runtime=%s", fnConfig.Runtime),
		"--label", fmt.Sprintf("io.enclii.function.handler=%s", fnConfig.Handler),
		"--label", fmt.Sprintf("io.enclii.service-id=%s", job.ServiceID.String()),
		"--label", fmt.Sprintf("io.enclii.release-id=%s", job.ReleaseID.String()),
	)

	// Add build context (entire build dir to include functions/)
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = buildDir

	if err := e.runWithLogs(cmd, job.ID); err != nil {
		return "", fmt.Errorf("function build failed: %w", err)
	}

	e.log(job.ID, "✅ Function image built: %s", imageTag)

	return imageTag, nil
}

// generateFunctionImageTag creates an image tag for functions
func (e *Executor) generateFunctionImageTag(job *queue.BuildJob) string {
	shortSHA := job.GitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	// Use 'fn' prefix to distinguish from regular services
	return fmt.Sprintf("%s/fn/%s/%s:%s",
		e.registry,
		job.ProjectID.String()[:8],
		job.ServiceID.String()[:8],
		shortSHA,
	)
}

// GetRuntimeDisplayName returns a human-readable name for the runtime
func GetRuntimeDisplayName(runtime FunctionRuntime) string {
	switch runtime {
	case RuntimeGo:
		return "Go (distroless)"
	case RuntimePython:
		return "Python 3.12"
	case RuntimeNode:
		return "Node.js 20"
	case RuntimeRust:
		return "Rust (musl)"
	default:
		return string(runtime)
	}
}

// GetRuntimeColdStartTarget returns the target cold start time for a runtime
func GetRuntimeColdStartTarget(runtime FunctionRuntime) string {
	switch runtime {
	case RuntimeGo:
		return "<500ms"
	case RuntimePython:
		return "<3s"
	case RuntimeNode:
		return "<2s"
	case RuntimeRust:
		return "<500ms"
	default:
		return "unknown"
	}
}

// ValidateFunctionStructure validates that the function directory has required files
func ValidateFunctionStructure(buildDir string) error {
	functionsDir := filepath.Join(buildDir, "functions")

	// Check functions directory exists
	if _, err := os.Stat(functionsDir); os.IsNotExist(err) {
		return fmt.Errorf("functions/ directory not found")
	}

	// Check for at least one runtime indicator
	indicators := map[string]FunctionRuntime{
		"go.mod":           RuntimeGo,
		"main.go":          RuntimeGo,
		"requirements.txt": RuntimePython,
		"handler.py":       RuntimePython,
		"package.json":     RuntimeNode,
		"handler.js":       RuntimeNode,
		"handler.ts":       RuntimeNode,
		"Cargo.toml":       RuntimeRust,
	}

	for file := range indicators {
		if _, err := os.Stat(filepath.Join(functionsDir, file)); err == nil {
			return nil // Found at least one indicator
		}
	}

	return fmt.Errorf("no recognized runtime files in functions/")
}

// ListDetectedFunctions scans a directory for multiple functions (future multi-function support)
func ListDetectedFunctions(buildDir string) ([]string, error) {
	functionsDir := filepath.Join(buildDir, "functions")

	entries, err := os.ReadDir(functionsDir)
	if err != nil {
		return nil, err
	}

	var functions []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Check if subdirectory has function indicators
			subDir := filepath.Join(functionsDir, entry.Name())
			if hasFunctionIndicators(subDir) {
				functions = append(functions, entry.Name())
			}
		}
	}

	// If no subdirectories with functions, treat root functions/ as single function
	if len(functions) == 0 && hasFunctionIndicators(functionsDir) {
		functions = append(functions, "default")
	}

	return functions, nil
}

func hasFunctionIndicators(dir string) bool {
	indicators := []string{
		"go.mod", "main.go",
		"requirements.txt", "handler.py",
		"package.json", "handler.js", "handler.ts",
		"Cargo.toml",
	}

	for _, ind := range indicators {
		if _, err := os.Stat(filepath.Join(dir, ind)); err == nil {
			return true
		}
	}
	return false
}
