package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/IceRhymers/databricks-claude/pkg/authcheck"
	"github.com/IceRhymers/databricks-claude/pkg/proxy"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	verbose, version, showHelp, printEnv, model, upstream, logFile, profile, proxyAPIKey, tlsCert, tlsKey, opencodeArgs := parseArgs(os.Args[1:])

	if showHelp {
		handleHelp(upstream)
		os.Exit(0)
	}

	if version {
		fmt.Printf("databricks-opencode %s\n", Version)
		os.Exit(0)
	}

	// Default: discard all logs (silent wrapper).
	log.SetOutput(io.Discard)

	if verbose {
		log.SetOutput(os.Stderr)
	}
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			log.SetOutput(os.Stderr)
			log.Fatalf("databricks-opencode: cannot open log file %q: %v", logFile, err)
		}
		defer f.Close()
		if verbose {
			log.SetOutput(io.MultiWriter(os.Stderr, f))
		} else {
			log.SetOutput(f)
		}
	}

	// --- Resolve profile ---
	// Resolution chain: --profile flag → env var → saved state → "DEFAULT".
	// When --profile is explicitly passed, save it for future sessions.
	profileExplicit := profile != ""
	if profile == "" {
		profile = os.Getenv("DATABRICKS_CONFIG_PROFILE")
	}
	if profile == "" {
		if saved := loadState(); saved.Profile != "" {
			profile = saved.Profile
			log.Printf("databricks-opencode: using saved profile: %s", profile)
		}
	}
	if profile == "" {
		profile = "DEFAULT"
	}
	if profileExplicit {
		saved := loadState()
		saved.Profile = profile
		if err := saveState(saved); err != nil {
			log.Printf("databricks-opencode: failed to save profile: %v", err)
		} else {
			log.Printf("databricks-opencode: saved profile %q for future sessions", profile)
		}
	}
	log.Printf("databricks-opencode: using profile: %s", profile)

	// --- Resolve model ---
	// Resolution chain: --model flag → saved state → "databricks-gpt-5-4" default.
	// When --model is explicitly passed, save it for future sessions.
	modelExplicit := model != ""
	if model == "" {
		if saved := loadState(); saved.Model != "" {
			model = saved.Model
			log.Printf("databricks-opencode: using saved model: %s", model)
		}
	}
	if model == "" {
		model = "databricks-gpt-5-4"
	}
	if modelExplicit {
		saved := loadState()
		saved.Model = model
		if err := saveState(saved); err != nil {
			log.Printf("databricks-opencode: failed to save model: %v", err)
		} else {
			log.Printf("databricks-opencode: saved model %q for future sessions", model)
		}
	}

	// --- Ensure the user is authenticated before proceeding ---
	if err := authcheck.EnsureAuthenticated(profile); err != nil {
		log.Fatalf("databricks-opencode: auth failed: %v", err)
	}

	// --- TLS validation ---
	if err := proxy.ValidateTLSConfig(tlsCert, tlsKey); err != nil {
		log.Fatalf("databricks-opencode: %v", err)
	}

	// --- Startup security checks ---
	for _, w := range proxy.SecurityChecks() {
		fmt.Fprintln(os.Stderr, w)
	}

	// --- Seed token cache ---
	tp := NewTokenProvider("", profile)
	initialToken, err := tp.Token(context.Background())
	if err != nil {
		log.Fatalf("databricks-opencode: failed to fetch initial token: %v", err)
	}

	// --- Discover host + construct gateway URL ---
	host, err := DiscoverHost("", profile)
	if err != nil {
		log.Fatalf("databricks-opencode: failed to discover host: %v\nRun 'databricks auth login' first", err)
	}
	log.Printf("databricks-opencode: discovered host: %s", host)

	gatewayURL := upstream
	if gatewayURL == "" {
		gatewayURL = ConstructGatewayURL(host, initialToken)
	}
	log.Printf("databricks-opencode: gateway URL: %s", gatewayURL)

	// --- Print env and exit if requested ---
	if printEnv {
		handlePrintEnv(host, gatewayURL, initialToken, profile)
		os.Exit(0)
	}

	// Verify opencode is on PATH before starting proxy.
	if _, err := exec.LookPath("opencode"); err != nil {
		log.Fatalf("databricks-opencode: opencode binary not found on PATH — install from https://opencode.ai")
	}

	// --- Start local proxy so the token stays fresh for the entire session ---
	// The proxy uses tokencache to refresh the Databricks OAuth token automatically
	// (5-min buffer before expiry). OpenCode talks to the proxy via config.json;
	// the proxy injects a fresh Bearer token on every outbound request to the
	// AI Gateway. HTTP/SSE only — OpenCode uses SSE, no WebSocket needed.
	proxyHandler := NewProxyServer(&ProxyConfig{
		InferenceUpstream: gatewayURL,
		TokenProvider:     tp,
		Verbose:           verbose,
		APIKey:            proxyAPIKey,
		TLSCertFile:       tlsCert,
		TLSKeyFile:        tlsKey,
	})
	listener, err := StartProxy(proxyHandler, tlsCert, tlsKey)
	if err != nil {
		log.Fatalf("databricks-opencode: failed to start proxy: %v", err)
	}
	defer listener.Close()
	proxyScheme := "http://"
	if tlsCert != "" && tlsKey != "" {
		proxyScheme = "https://"
	}
	proxyAddr := proxyScheme + listener.Addr().String()
	log.Printf("databricks-opencode: local proxy %s -> %s", proxyAddr, gatewayURL)

	// --- Patch config.json to point OpenCode at the local proxy ---
	cm := NewConfigManager()
	if err := cm.Setup(proxyAddr, model, "databricks-proxy", modelExplicit); err != nil {
		log.Fatalf("databricks-opencode: failed to patch config.json: %v", err)
	}

	// Set OPENAI_API_KEY as a placeholder — the proxy overwrites the
	// Authorization header with a live Databricks token per request.
	os.Setenv("OPENAI_API_KEY", "databricks-proxy")

	log.Printf("databricks-opencode: launching opencode")

	// --- Run opencode as a child process (parent stays alive to serve the proxy) ---
	exitCode, err := RunOpenCode(context.Background(), opencodeArgs)

	// Explicitly restore config.json before exiting. This is NOT deferred
	// because os.Exit() skips deferred functions — we must restore before
	// exit to avoid leaving config.json pointing at a dead proxy.
	cm.Restore()

	if err != nil {
		log.Fatalf("databricks-opencode: opencode failed: %v", err)
	}
	os.Exit(exitCode)
}

// parseArgs separates databricks-opencode flags from opencode flags.
func parseArgs(args []string) (verbose bool, version bool, showHelp bool, printEnv bool, model string, upstream string, logFile string, profile string, proxyAPIKey string, tlsCert string, tlsKey string, opencodeArgs []string) {
	knownFlags := map[string]bool{
		"--verbose":       true,
		"--version":       true,
		"--help":          true,
		"--print-env":     true,
		"--model":         true,
		"--upstream":      true,
		"--log-file":      true,
		"--profile":       true,
		"--proxy-api-key": true,
		"--tls-cert":      true,
		"--tls-key":       true,
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		// Explicit separator: everything after "--" goes to opencode.
		if arg == "--" {
			opencodeArgs = append(opencodeArgs, args[i+1:]...)
			return
		}

		if arg == "-h" {
			showHelp = true
			i++
			continue
		}
		if arg == "-v" {
			verbose = true
			i++
			continue
		}

		if strings.HasPrefix(arg, "--") {
			name := arg
			value := ""
			if eqIdx := strings.Index(arg, "="); eqIdx >= 0 {
				name = arg[:eqIdx]
				value = arg[eqIdx+1:]
			}

			if knownFlags[name] {
				switch name {
				case "--model":
					if value != "" {
						model = value
					} else if i+1 < len(args) {
						i++
						model = args[i]
					}
				case "--upstream":
					if value != "" {
						upstream = value
					} else if i+1 < len(args) {
						i++
						upstream = args[i]
					}
				case "--log-file":
					if value != "" {
						logFile = value
					} else if i+1 < len(args) {
						i++
						logFile = args[i]
					}
				case "--profile":
					if value != "" {
						profile = value
					} else if i+1 < len(args) {
						i++
						profile = args[i]
					}
				case "--proxy-api-key":
					if value != "" {
						proxyAPIKey = value
					} else if i+1 < len(args) {
						i++
						proxyAPIKey = args[i]
					}
				case "--tls-cert":
					if value != "" {
						tlsCert = value
					} else if i+1 < len(args) {
						i++
						tlsCert = args[i]
					}
				case "--tls-key":
					if value != "" {
						tlsKey = value
					} else if i+1 < len(args) {
						i++
						tlsKey = args[i]
					}
				case "--verbose":
					verbose = true
				case "--version":
					version = true
				case "--help":
					showHelp = true
				case "--print-env":
					printEnv = true
				}
				i++
				continue
			}
		}

		// Not a known flag — pass through to opencode.
		opencodeArgs = append(opencodeArgs, arg)
		i++
	}
	return
}

// handleHelp prints the databricks-opencode help section, then execs opencode --help.
func handleHelp(upstreamBinary string) {
	fmt.Printf(`databricks-opencode v%s — Databricks AI Gateway wrapper for OpenCode CLI

Patches ~/.config/opencode/config.json and runs a local proxy so the OpenCode CLI
authenticates through a Databricks AI Gateway endpoint with live token refresh.

Usage:
  databricks-opencode [databricks-opencode flags] [opencode flags] [opencode args]

Databricks-OpenCode Flags:
  --profile string      Databricks CLI profile (saved for future sessions; default: env or "DEFAULT")
  --upstream string     Override the AI Gateway URL (default: auto-discovered)
  --model string        Model to use (default: "databricks-gpt-5-4")
  --print-env           Print resolved configuration and exit (token redacted)
  --verbose, -v         Enable debug logging to stderr
  --log-file string     Write debug logs to a file (combinable with --verbose)
  --proxy-api-key string    Require this API key on all proxy requests (default: disabled)
  --tls-cert string         Path to TLS certificate file (requires --tls-key)
  --tls-key string          Path to TLS private key file (requires --tls-cert)
  --version             Print version and exit
  --help, -h            Show this help message

────────────────────────────────────────────────────────────────────────────────
OpenCode CLI Options:
`, Version)

	opencodeBin := upstreamBinary
	if opencodeBin == "" {
		if p, err := exec.LookPath("opencode"); err == nil {
			opencodeBin = p
		}
	}

	if opencodeBin == "" {
		fmt.Println("(opencode binary not found on PATH — install from https://opencode.ai)")
		return
	}

	var buf bytes.Buffer
	cmd := exec.Command(opencodeBin, "--help")
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	fmt.Print(buf.String())
}

// handlePrintEnv prints resolved configuration with the token redacted.
func handlePrintEnv(databricksHost, openaiBaseURL, token, profile string) {
	redacted := "**** (redacted)"
	if strings.HasPrefix(token, "dapi-") {
		redacted = "dapi-***"
	}

	opencodePath := "(not found)"
	if p, err := exec.LookPath("opencode"); err == nil {
		opencodePath = p
	}

	fmt.Printf(`databricks-opencode configuration:
  Profile:           %s
  DATABRICKS_HOST:   %s
  OPENAI_BASE_URL:   %s
  OPENAI_API_KEY:    %s
  OpenCode binary:   %s
`, profile, databricksHost, openaiBaseURL, redacted, opencodePath)
}
