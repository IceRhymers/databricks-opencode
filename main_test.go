package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
)

// --- parseArgs tests ---

func TestParseArgs_HelpLong(t *testing.T) {
	verbose, version, showHelp, printEnv, model, upstream, logFile, profile, _, _, _, _, opencodeArgs := parseArgs([]string{"--help"})
	if !showHelp {
		t.Error("expected showHelp=true for --help")
	}
	if verbose || version || printEnv || model != "" || upstream != "" || logFile != "" || profile != "" || len(opencodeArgs) != 0 {
		t.Error("unexpected non-default values alongside --help")
	}
}

func TestParseArgs_HelpShort(t *testing.T) {
	_, _, showHelp, _, _, _, _, _, _, _, _, _, _ := parseArgs([]string{"-h"})
	if !showHelp {
		t.Error("expected showHelp=true for -h")
	}
}

func TestParseArgs_PrintEnv(t *testing.T) {
	_, _, _, printEnv, _, _, _, _, _, _, _, _, _ := parseArgs([]string{"--print-env"})
	if !printEnv {
		t.Error("expected printEnv=true for --print-env")
	}
}

func TestParseArgs_Version(t *testing.T) {
	_, version, _, _, _, _, _, _, _, _, _, _, _ := parseArgs([]string{"--version"})
	if !version {
		t.Error("expected version=true for --version")
	}
}

func TestParseArgs_Verbose(t *testing.T) {
	verbose, _, _, _, _, _, _, _, _, _, _, _, _ := parseArgs([]string{"--verbose"})
	if !verbose {
		t.Error("expected verbose=true for --verbose")
	}
}

func TestParseArgs_VerboseShort(t *testing.T) {
	verbose, _, _, _, _, _, _, _, _, _, _, _, _ := parseArgs([]string{"-v"})
	if !verbose {
		t.Error("expected verbose=true for -v")
	}
}

func TestParseArgs_LogFile(t *testing.T) {
	_, _, _, _, _, _, logFile, _, _, _, _, _, _ := parseArgs([]string{"--log-file", "/tmp/test.log"})
	if logFile != "/tmp/test.log" {
		t.Errorf("expected logFile=%q, got %q", "/tmp/test.log", logFile)
	}
}

func TestParseArgs_LogFileEquals(t *testing.T) {
	_, _, _, _, _, _, logFile, _, _, _, _, _, _ := parseArgs([]string{"--log-file=/tmp/test.log"})
	if logFile != "/tmp/test.log" {
		t.Errorf("expected logFile=%q, got %q", "/tmp/test.log", logFile)
	}
}

func TestParseArgs_Upstream(t *testing.T) {
	_, _, _, _, _, upstream, _, _, _, _, _, _, _ := parseArgs([]string{"--upstream", "https://gw.example.com/openai/v1"})
	if upstream != "https://gw.example.com/openai/v1" {
		t.Errorf("expected upstream=%q, got %q", "https://gw.example.com/openai/v1", upstream)
	}
}

func TestParseArgs_UpstreamEquals(t *testing.T) {
	_, _, _, _, _, upstream, _, _, _, _, _, _, _ := parseArgs([]string{"--upstream=https://gw.example.com/openai/v1"})
	if upstream != "https://gw.example.com/openai/v1" {
		t.Errorf("expected upstream=%q, got %q", "https://gw.example.com/openai/v1", upstream)
	}
}

func TestParseArgs_Model(t *testing.T) {
	_, _, _, _, model, _, _, _, _, _, _, _, _ := parseArgs([]string{"--model", "gpt-4o"})
	if model != "gpt-4o" {
		t.Errorf("expected model=%q, got %q", "gpt-4o", model)
	}
}

func TestParseArgs_ModelEquals(t *testing.T) {
	_, _, _, _, model, _, _, _, _, _, _, _, _ := parseArgs([]string{"--model=gpt-4o"})
	if model != "gpt-4o" {
		t.Errorf("expected model=%q, got %q", "gpt-4o", model)
	}
}

func TestParseArgs_UnknownFlagPassthrough(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, opencodeArgs := parseArgs([]string{"--unknown"})
	if len(opencodeArgs) != 1 || opencodeArgs[0] != "--unknown" {
		t.Errorf("expected opencodeArgs=[\"--unknown\"], got %v", opencodeArgs)
	}
}

func TestParseArgs_EmptyArgs(t *testing.T) {
	verbose, version, showHelp, printEnv, model, upstream, logFile, profile, _, _, _, _, opencodeArgs := parseArgs([]string{})
	if verbose || version || showHelp || printEnv {
		t.Error("expected all bool flags false for empty args")
	}
	if model != "" {
		t.Errorf("expected empty model, got %q", model)
	}
	if upstream != "" {
		t.Errorf("expected empty upstream, got %q", upstream)
	}
	if logFile != "" {
		t.Errorf("expected empty logFile, got %q", logFile)
	}
	if profile != "" {
		t.Errorf("expected empty profile, got %q", profile)
	}
	if len(opencodeArgs) != 0 {
		t.Errorf("expected no opencodeArgs, got %v", opencodeArgs)
	}
}

func TestParseArgs_Mixed(t *testing.T) {
	verbose, _, showHelp, _, _, upstream, _, _, _, _, _, _, _ := parseArgs([]string{"--verbose", "--upstream", "https://gw.example.com", "--help"})
	if !showHelp {
		t.Error("expected showHelp=true")
	}
	if !verbose {
		t.Error("expected verbose=true")
	}
	if upstream != "https://gw.example.com" {
		t.Errorf("expected upstream=%q, got %q", "https://gw.example.com", upstream)
	}
}

func TestParseArgs_Separator(t *testing.T) {
	verbose, _, _, _, _, _, _, _, _, _, _, _, opencodeArgs := parseArgs([]string{"--verbose", "--", "--unknown", "arg1"})
	if !verbose {
		t.Error("expected verbose=true before separator")
	}
	if len(opencodeArgs) != 2 || opencodeArgs[0] != "--unknown" || opencodeArgs[1] != "arg1" {
		t.Errorf("expected opencodeArgs=[\"--unknown\", \"arg1\"], got %v", opencodeArgs)
	}
}

func TestParseArgs_PassthroughArgs(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, _, opencodeArgs := parseArgs([]string{"prompt text", "--some-flag", "value"})
	if len(opencodeArgs) != 3 {
		t.Errorf("expected 3 opencodeArgs, got %d: %v", len(opencodeArgs), opencodeArgs)
	}
}

// Table-driven comprehensive test for parseArgs.
func TestParseArgs_Table(t *testing.T) {
	type result struct {
		verbose     bool
		version     bool
		showHelp    bool
		printEnv    bool
		model       string
		upstream    string
		logFile     string
		profile     string
		opencodeLen int
	}

	tests := []struct {
		name string
		args []string
		want result
	}{
		{
			name: "--help sets showHelp",
			args: []string{"--help"},
			want: result{showHelp: true},
		},
		{
			name: "-h sets showHelp",
			args: []string{"-h"},
			want: result{showHelp: true},
		},
		{
			name: "--print-env sets printEnv",
			args: []string{"--print-env"},
			want: result{printEnv: true},
		},
		{
			name: "--version sets version",
			args: []string{"--version"},
			want: result{version: true},
		},
		{
			name: "--verbose sets verbose",
			args: []string{"--verbose"},
			want: result{verbose: true},
		},
		{
			name: "-v sets verbose",
			args: []string{"-v"},
			want: result{verbose: true},
		},
		{
			name: "--log-file sets logFile",
			args: []string{"--log-file", "/tmp/test.log"},
			want: result{logFile: "/tmp/test.log"},
		},
		{
			name: "--log-file=value sets logFile",
			args: []string{"--log-file=/tmp/test.log"},
			want: result{logFile: "/tmp/test.log"},
		},
		{
			name: "--upstream sets upstream",
			args: []string{"--upstream", "https://gw.example.com"},
			want: result{upstream: "https://gw.example.com"},
		},
		{
			name: "--model sets model",
			args: []string{"--model", "custom-model"},
			want: result{model: "custom-model"},
		},
		{
			name: "unknown flag passes through",
			args: []string{"--unknown"},
			want: result{opencodeLen: 1},
		},
		{
			name: "empty args all defaults",
			args: []string{},
			want: result{},
		},
		{
			name: "mixed flags: verbose, upstream, help",
			args: []string{"--verbose", "--upstream", "https://gw.example.com", "--help"},
			want: result{showHelp: true, verbose: true, upstream: "https://gw.example.com"},
		},
		{
			name: "--profile sets profile",
			args: []string{"--profile", "aidev"},
			want: result{profile: "aidev"},
		},
		{
			name: "--profile=value sets profile",
			args: []string{"--profile=aidev"},
			want: result{profile: "aidev"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verbose, version, showHelp, printEnv, model, upstream, logFile, profile, _, _, _, _, opencodeArgs := parseArgs(tc.args)

			if verbose != tc.want.verbose {
				t.Errorf("verbose: got %v, want %v", verbose, tc.want.verbose)
			}
			if version != tc.want.version {
				t.Errorf("version: got %v, want %v", version, tc.want.version)
			}
			if showHelp != tc.want.showHelp {
				t.Errorf("showHelp: got %v, want %v", showHelp, tc.want.showHelp)
			}
			if printEnv != tc.want.printEnv {
				t.Errorf("printEnv: got %v, want %v", printEnv, tc.want.printEnv)
			}
			if model != tc.want.model {
				t.Errorf("model: got %q, want %q", model, tc.want.model)
			}
			if upstream != tc.want.upstream {
				t.Errorf("upstream: got %q, want %q", upstream, tc.want.upstream)
			}
			if logFile != tc.want.logFile {
				t.Errorf("logFile: got %q, want %q", logFile, tc.want.logFile)
			}
			if profile != tc.want.profile {
				t.Errorf("profile: got %q, want %q", profile, tc.want.profile)
			}
			if len(opencodeArgs) != tc.want.opencodeLen {
				t.Errorf("opencodeArgs length: got %d, want %d (args: %v)", len(opencodeArgs), tc.want.opencodeLen, opencodeArgs)
			}
		})
	}
}

// --- default log discard test ---

func TestDefaultLogDiscard(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	var buf bytes.Buffer
	log.SetOutput(io.Discard)
	log.Print("this should be discarded")

	log.SetOutput(&buf)
	log.Print("this should appear")

	if !strings.Contains(buf.String(), "this should appear") {
		t.Error("expected log output after switching from Discard")
	}
}

// --- handlePrintEnv tests ---

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestHandlePrintEnv_DapiTokenRedacted(t *testing.T) {
	out := captureStdout(func() {
		handlePrintEnv("https://dbc.example.com", "https://gw.example.com/openai/v1", "dapi-abc123secret", "DEFAULT")
	})
	if !strings.Contains(out, "dapi-***") {
		t.Errorf("expected dapi token to appear as 'dapi-***', got:\n%s", out)
	}
	if strings.Contains(out, "dapi-abc123secret") {
		t.Errorf("raw dapi token should not appear in output, got:\n%s", out)
	}
}

func TestHandlePrintEnv_NonDapiTokenRedacted(t *testing.T) {
	out := captureStdout(func() {
		handlePrintEnv("https://dbc.example.com", "https://gw.example.com/openai/v1", "eyJhbGciOiJSUzI1NiJ9", "DEFAULT")
	})
	if !strings.Contains(out, "**** (redacted)") {
		t.Errorf("expected non-dapi token to appear as '**** (redacted)', got:\n%s", out)
	}
}

func TestHandlePrintEnv_ContainsProfile(t *testing.T) {
	out := captureStdout(func() {
		handlePrintEnv("https://dbc.example.com", "https://gw.example.com/openai/v1", "tok", "aidev")
	})
	if !strings.Contains(out, "aidev") {
		t.Errorf("expected output to contain profile 'aidev', got:\n%s", out)
	}
}

func TestHandlePrintEnv_ContainsDatabricksHost(t *testing.T) {
	host := "https://dbc-abc123.cloud.databricks.com"
	out := captureStdout(func() {
		handlePrintEnv(host, "https://gw.example.com/openai/v1", "tok", "DEFAULT")
	})
	if !strings.Contains(out, host) {
		t.Errorf("expected output to contain DATABRICKS_HOST %q, got:\n%s", host, out)
	}
}

func TestHandlePrintEnv_ContainsOpenAIBaseURL(t *testing.T) {
	baseURL := "https://gw.example.com/openai/v1"
	out := captureStdout(func() {
		handlePrintEnv("https://dbc.example.com", baseURL, "tok", "DEFAULT")
	})
	if !strings.Contains(out, baseURL) {
		t.Errorf("expected output to contain OPENAI_BASE_URL %q, got:\n%s", baseURL, out)
	}
}

// --- handleHelp tests ---

func TestHandleHelp_ContainsDatabricksOpencode(t *testing.T) {
	out := captureStdout(func() {
		handleHelp("")
	})
	if !strings.Contains(out, "databricks-opencode") {
		t.Errorf("expected help output to contain 'databricks-opencode', got:\n%s", out)
	}
}

func TestHandleHelp_ContainsPrintEnvFlag(t *testing.T) {
	out := captureStdout(func() {
		handleHelp("")
	})
	if !strings.Contains(out, "--print-env") {
		t.Errorf("expected help output to contain '--print-env', got:\n%s", out)
	}
}

func TestHandleHelp_ContainsOpenCodeCLISeparator(t *testing.T) {
	out := captureStdout(func() {
		handleHelp("")
	})
	if !strings.Contains(out, "OpenCode CLI Options:") {
		t.Errorf("expected help output to contain 'OpenCode CLI Options:', got:\n%s", out)
	}
}

func TestParseArgs_Profile(t *testing.T) {
	_, _, _, _, _, _, _, profile, _, _, _, _, _ := parseArgs([]string{"--profile", "aidev"})
	if profile != "aidev" {
		t.Errorf("expected profile=%q, got %q", "aidev", profile)
	}
}

func TestParseArgs_ProfileEquals(t *testing.T) {
	_, _, _, _, _, _, _, profile, _, _, _, _, _ := parseArgs([]string{"--profile=production"})
	if profile != "production" {
		t.Errorf("expected profile=%q, got %q", "production", profile)
	}
}

func TestParseArgs_Port(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, port, _ := parseArgs([]string{"--port", "8080"})
	if port != 8080 {
		t.Errorf("expected port=8080, got %d", port)
	}
}

func TestParseArgs_PortEquals(t *testing.T) {
	_, _, _, _, _, _, _, _, _, _, _, port, _ := parseArgs([]string{"--port=9000"})
	if port != 9000 {
		t.Errorf("expected port=9000, got %d", port)
	}
}

func TestHandleHelp_AllFlagsPresent(t *testing.T) {
	out := captureStdout(func() {
		handleHelp("")
	})
	flags := []string{"--profile", "--upstream", "--verbose", "-v", "--log-file", "--model", "--version", "--help", "--port"}
	for _, flag := range flags {
		if !strings.Contains(out, flag) {
			t.Errorf("expected help output to contain flag %q, got:\n%s", flag, out)
		}
	}
}

func TestHandleHelp_ContainsVersion(t *testing.T) {
	out := captureStdout(func() {
		handleHelp("")
	})
	if !strings.Contains(out, fmt.Sprintf("databricks-opencode v%s", Version)) {
		t.Errorf("expected help output to contain version string, got:\n%s", out)
	}
}
