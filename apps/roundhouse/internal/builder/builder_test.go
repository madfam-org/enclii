package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- BuildMode constants ---

func TestBuildModeConstants(t *testing.T) {
	if BuildModeDocker != "docker" {
		t.Errorf("BuildModeDocker: got %q, want %q", BuildModeDocker, "docker")
	}
	if BuildModeKaniko != "kaniko" {
		t.Errorf("BuildModeKaniko: got %q, want %q", BuildModeKaniko, "kaniko")
	}
}

// --- IsFunctionBuild ---

func TestIsFunctionBuild(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  bool
	}{
		{
			name: "functions_directory_exists",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "functions"), 0755)
			},
			want: true,
		},
		{
			name:  "no_functions_directory",
			setup: func(dir string) {},
			want:  false,
		},
		{
			name: "functions_is_a_file_not_dir",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "functions"), []byte("not a dir"), 0644)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)
			got := IsFunctionBuild(dir)
			if got != tt.want {
				t.Errorf("IsFunctionBuild() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DetectFunctionRuntime ---

func TestDetectFunctionRuntime(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		wantRuntime FunctionRuntime
		wantHandler string
		wantErr     bool
	}{
		{
			name:        "go_with_go_mod",
			files:       map[string]string{"go.mod": "module test"},
			wantRuntime: RuntimeGo,
			wantHandler: "main.Handler",
		},
		{
			name:        "go_with_main_go",
			files:       map[string]string{"main.go": "package main"},
			wantRuntime: RuntimeGo,
			wantHandler: "main.Handler",
		},
		{
			name:        "python_with_requirements",
			files:       map[string]string{"requirements.txt": "flask"},
			wantRuntime: RuntimePython,
			wantHandler: "handler.main",
		},
		{
			name:        "python_with_handler",
			files:       map[string]string{"handler.py": "def main(): pass"},
			wantRuntime: RuntimePython,
			wantHandler: "handler.main",
		},
		{
			name:        "node_with_package_json",
			files:       map[string]string{"package.json": "{}"},
			wantRuntime: RuntimeNode,
			wantHandler: "handler.main",
		},
		{
			name:        "node_with_handler_js",
			files:       map[string]string{"handler.js": "exports.main = () => {}"},
			wantRuntime: RuntimeNode,
			wantHandler: "handler.main",
		},
		{
			name:        "node_typescript_preferred",
			files:       map[string]string{"package.json": "{}", "handler.ts": "export const main = () => {}"},
			wantRuntime: RuntimeNode,
			wantHandler: "handler.main",
		},
		{
			name:        "rust_with_cargo",
			files:       map[string]string{"Cargo.toml": "[package]"},
			wantRuntime: RuntimeRust,
			wantHandler: "handler",
		},
		{
			name:    "no_recognized_files",
			files:   map[string]string{"random.txt": "nothing here"},
			wantErr: true,
		},
		{
			name:    "empty_functions_dir",
			files:   map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			fnDir := filepath.Join(dir, "functions")
			os.MkdirAll(fnDir, 0755)
			for name, content := range tt.files {
				os.WriteFile(filepath.Join(fnDir, name), []byte(content), 0644)
			}

			config, err := DetectFunctionRuntime(dir)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config.Runtime != tt.wantRuntime {
				t.Errorf("Runtime: got %q, want %q", config.Runtime, tt.wantRuntime)
			}
			if config.Handler != tt.wantHandler {
				t.Errorf("Handler: got %q, want %q", config.Handler, tt.wantHandler)
			}
			if config.FunctionsDir != fnDir {
				t.Errorf("FunctionsDir: got %q, want %q", config.FunctionsDir, fnDir)
			}
		})
	}
}

// --- GetFunctionDockerfile ---

func TestGetFunctionDockerfile(t *testing.T) {
	tests := []struct {
		name         string
		runtime      FunctionRuntime
		wantContains string
		wantNonEmpty bool
	}{
		{"go", RuntimeGo, "golang:", true},
		{"python", RuntimePython, "python:", true},
		{"node", RuntimeNode, "node:", true},
		{"rust", RuntimeRust, "rust:", true},
		{"unknown", FunctionRuntime("unknown"), "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			df := GetFunctionDockerfile(tt.runtime)
			if tt.wantNonEmpty && df == "" {
				t.Error("expected non-empty Dockerfile")
			}
			if !tt.wantNonEmpty && df != "" {
				t.Errorf("expected empty Dockerfile for unknown runtime, got length %d", len(df))
			}
			if tt.wantContains != "" && !strings.Contains(df, tt.wantContains) {
				t.Errorf("Dockerfile should contain %q", tt.wantContains)
			}
		})
	}
}

// --- GetRuntimeDisplayName ---

func TestGetRuntimeDisplayName(t *testing.T) {
	tests := []struct {
		runtime FunctionRuntime
		want    string
	}{
		{RuntimeGo, "Go (distroless)"},
		{RuntimePython, "Python 3.12"},
		{RuntimeNode, "Node.js 20"},
		{RuntimeRust, "Rust (musl)"},
		{FunctionRuntime("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.runtime), func(t *testing.T) {
			got := GetRuntimeDisplayName(tt.runtime)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- GetRuntimeColdStartTarget ---

func TestGetRuntimeColdStartTarget(t *testing.T) {
	tests := []struct {
		runtime FunctionRuntime
		want    string
	}{
		{RuntimeGo, "<500ms"},
		{RuntimePython, "<3s"},
		{RuntimeNode, "<2s"},
		{RuntimeRust, "<500ms"},
		{FunctionRuntime("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.runtime), func(t *testing.T) {
			got := GetRuntimeColdStartTarget(tt.runtime)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- ValidateFunctionStructure ---

func TestValidateFunctionStructure(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(dir string)
		wantErr bool
	}{
		{
			name: "valid_go_function",
			setup: func(dir string) {
				fnDir := filepath.Join(dir, "functions")
				os.MkdirAll(fnDir, 0755)
				os.WriteFile(filepath.Join(fnDir, "main.go"), []byte("package main"), 0644)
			},
			wantErr: false,
		},
		{
			name: "valid_python_function",
			setup: func(dir string) {
				fnDir := filepath.Join(dir, "functions")
				os.MkdirAll(fnDir, 0755)
				os.WriteFile(filepath.Join(fnDir, "handler.py"), []byte("def main(): pass"), 0644)
			},
			wantErr: false,
		},
		{
			name:    "no_functions_directory",
			setup:   func(dir string) {},
			wantErr: true,
		},
		{
			name: "empty_functions_directory",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "functions"), 0755)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)
			err := ValidateFunctionStructure(dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFunctionStructure() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- FunctionRuntime constants ---

func TestFunctionRuntimeConstants(t *testing.T) {
	tests := []struct {
		got  FunctionRuntime
		want string
	}{
		{RuntimeGo, "go"},
		{RuntimePython, "python"},
		{RuntimeNode, "node"},
		{RuntimeRust, "rust"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
