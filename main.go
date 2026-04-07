package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IceRhymers/databricks-claude/pkg/authcheck"
	"github.com/IceRhymers/databricks-claude/pkg/portbind"
	"github.com/IceRhymers/databricks-claude/pkg/proxy"
	"github.com/IceRhymers/databricks-claude/pkg/refcount"
	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	verbose, version, showHelp, printEnv, model, upstream, logFile, profile, proxyAPIKey, tlsCert, tlsKey, portFlag, headless, opencodeArgs := parseArgs(os.Args[1:])

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
	// Resolution chain: --profile flag → saved state → "DEFAULT".
	// The env var DATABRICKS_CONFIG_PROFILE is intentionally NOT checked here
	// because external tools (e.g. Claude's settings.json) can inject it into
	// the process environment, silently overriding the user's saved proxy profile.
	// When --profile is explicitly passed, save it for future sessions.
	profileExplicit := profile != ""
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
	// Resolution chain: --model flag → saved state → "databricks-claude-sonnet-4-6" default.
	// When --model is explicitly passed, save it for future sessions.
	modelExplicit := model != ""
	if model == "" {
		if saved := loadState(); saved.Model != "" {
			model = saved.Model
			log.Printf("databricks-opencode: using saved model: %s", model)
		}
	}
	if model == "" {
		model = "databricks-claude-sonnet-4-6"
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

	// --- Load state and resolve port ---
	// Resolution chain: --port flag → saved state → defaultPort (49155).
	savedState := loadState()
	port := resolvePort(portFlag, savedState)
	portExplicit := portFlag > 0
	if portExplicit {
		savedState.Port = port
		if err := saveState(savedState); err != nil {
			log.Printf("databricks-opencode: failed to save port: %v", err)
		} else {
			log.Printf("databricks-opencode: saved port %d for future sessions", port)
		}
	}
	log.Printf("databricks-opencode: using port: %d", port)

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
		handlePrintEnv(host, gatewayURL, initialToken, profile, model)
		os.Exit(0)
	}

	// Verify opencode is on PATH before starting proxy (skip in headless mode).
	if !headless {
		if _, err := exec.LookPath("opencode"); err != nil {
			log.Fatalf("databricks-opencode: opencode binary not found on PATH — install from https://opencode.ai")
		}
	}

	// --- Bind to fixed port (or health-check existing owner) ---
	listener, isOwner, err := portbind.Bind("databricks-opencode", port)
	if err != nil {
		log.Fatalf("databricks-opencode: failed to bind port %d: %v", port, err)
	}

	// --- Build proxy handler (needed by both owner and watchdog) ---
	proxyHandler := NewProxyServer(&ProxyConfig{
		InferenceUpstream: gatewayURL,
		TokenProvider:     tp,
		Verbose:           verbose,
		APIKey:            proxyAPIKey,
		TLSCertFile:       tlsCert,
		TLSKeyFile:        tlsKey,
	})

	// --- If we own the listener, start the proxy on it ---
	if isOwner {
		servedLn, err := proxy.Serve(listener, proxyHandler, tlsCert, tlsKey)
		if err != nil {
			log.Fatalf("databricks-opencode: failed to start proxy: %v", err)
		}
		listener = servedLn
		log.Printf("databricks-opencode: proxy owner on :%d", port)
	} else {
		log.Printf("databricks-opencode: joining existing proxy on :%d", port)
		// Watch for owner death and take over the proxy if needed.
		go watchProxy(port, proxyHandler, tlsCert, tlsKey)
	}

	proxyScheme := "http"
	if tlsCert != "" && tlsKey != "" {
		proxyScheme = "https"
	}
	proxyAddr := fmt.Sprintf("%s://127.0.0.1:%d", proxyScheme, listenerPort(listener, port))
	log.Printf("databricks-opencode: local proxy %s -> %s", proxyAddr, gatewayURL)

	// --- Acquire refcount ---
	refcountPath := filepath.Join(os.TempDir(), fmt.Sprintf(".databricks-opencode-sessions-%d", port))
	if err := refcount.Acquire(refcountPath); err != nil {
		log.Printf("databricks-opencode: refcount acquire warning: %v", err)
	}

	// --- Ensure config.json points at the local proxy (idempotent) ---
	// Use proxyAPIKey if explicitly set; otherwise use a fixed placeholder.
	// The proxy rewrites auth headers with a live Databricks token — the
	// value here just needs to be non-empty for the @ai-sdk/anthropic provider.
	configAPIKey := proxyAPIKey
	if configAPIKey == "" {
		configAPIKey = "databricks-proxy"
	}
	if err := EnsureConfig(jsonconfig.New(), proxyAddr, model, configAPIKey, modelExplicit); err != nil {
		if headless {
			log.Printf("databricks-opencode: WARNING: failed to configure opencode: %v", err)
		} else {
			log.Fatalf("databricks-opencode: failed to configure opencode: %v", err)
		}
	}

	// --- Headless mode: print proxy URL and block until signal ---
	if headless {
		runHeadless(proxyAddr, listener, isOwner, refcountPath)
		return
	}

	log.Printf("databricks-opencode: launching opencode")

	// --- Run opencode as a child process (parent stays alive to serve the proxy) ---
	exitCode, runErr := RunOpenCode(context.Background(), opencodeArgs)

	// --- Release refcount; if last session and owner, close listener ---
	remaining, err := refcount.Release(refcountPath)
	if err != nil {
		log.Printf("databricks-opencode: refcount release warning: %v", err)
	}
	if remaining == 0 && isOwner {
		listener.Close()
		log.Printf("databricks-opencode: last session, proxy shut down")
	}

	if runErr != nil {
		log.Fatalf("databricks-opencode: opencode failed: %v", runErr)
	}
	os.Exit(exitCode)
}

// parseArgs separates databricks-opencode flags from opencode flags.
func parseArgs(args []string) (verbose bool, version bool, showHelp bool, printEnv bool, model string, upstream string, logFile string, profile string, proxyAPIKey string, tlsCert string, tlsKey string, port int, headless bool, opencodeArgs []string) {
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
		"--port":          true,
		"--headless":      true,
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
				case "--port":
					if value != "" {
						port, _ = strconv.Atoi(value)
					} else if i+1 < len(args) {
						i++
						port, _ = strconv.Atoi(args[i])
					}
				case "--verbose":
					verbose = true
				case "--version":
					version = true
				case "--help":
					showHelp = true
				case "--print-env":
					printEnv = true
				case "--headless":
					headless = true
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

// runHeadless starts the proxy without launching the opencode child process.
// It prints the proxy URL to stdout and blocks until SIGINT or SIGTERM.
func runHeadless(proxyURL string, ln net.Listener, isOwner bool, refcountPath string) {
	fmt.Printf("PROXY_URL=%s\n", proxyURL)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	signal.Stop(sigCh)
	n, _ := refcount.Release(refcountPath)
	if n == 0 && isOwner {
		ln.Close()
	}
}

// handleHelp prints the databricks-opencode help section, then execs opencode --help.
func handleHelp(upstreamBinary string) {
	fmt.Printf(`databricks-opencode v%s — Databricks AI Gateway wrapper for OpenCode CLI

Patches ~/.config/opencode/opencode.json and runs a local proxy so the OpenCode CLI
authenticates through a Databricks AI Gateway endpoint with live token refresh.

Usage:
  databricks-opencode [databricks-opencode flags] [opencode flags] [opencode args]

Databricks-OpenCode Flags:
  --profile string      Databricks CLI profile (saved for future sessions; default: env or "DEFAULT")
  --upstream string     Override the AI Gateway URL (default: auto-discovered)
  --model string        Model to use (default: "databricks-claude-sonnet-4-6")
  --print-env           Print resolved configuration and exit (token redacted)
  --verbose, -v         Enable debug logging to stderr
  --log-file string     Write debug logs to a file (combinable with --verbose)
  --proxy-api-key string    Require this API key on all proxy requests (default: disabled)
  --tls-cert string         Path to TLS certificate file (requires --tls-key)
  --tls-key string          Path to TLS private key file (requires --tls-cert)
  --port int                Local proxy port (default: 49155, saved for future sessions)
  --headless            Start proxy without launching opencode (for IDE extensions)
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

// proxyHealthy checks whether the proxy on the given port is responding.
func proxyHealthy(port int, scheme string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	if scheme == "https" {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	resp, err := client.Get(fmt.Sprintf("%s://127.0.0.1:%d/health", scheme, port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// watchProxy polls the proxy health endpoint and takes over the port if the
// owner process dies. Runs as a goroutine for non-owner sessions.
func watchProxy(port int, handler http.Handler, tlsCert, tlsKey string) {
	scheme := "http"
	if tlsCert != "" && tlsKey != "" {
		scheme = "https"
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if proxyHealthy(port, scheme) {
			continue
		}

		// Proxy is unreachable — try to bind the port and take over.
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue // another session grabbed it first
		}
		if _, err := proxy.Serve(ln, handler, tlsCert, tlsKey); err != nil {
			ln.Close()
			continue
		}
		log.Printf("databricks-opencode: proxy owner died, took over on :%d", port)
		return
	}
}

// listenerPort returns the port from the listener, or fallback if ln is nil.
func listenerPort(ln net.Listener, fallback int) int {
	if ln == nil {
		return fallback
	}
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return fallback
}

// handlePrintEnv prints resolved configuration with the token redacted.
func handlePrintEnv(databricksHost, openaiBaseURL, token, profile, model string) {
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
  Model:             %s
  DATABRICKS_HOST:   %s
  ANTHROPIC_BASE_URL: %s
  Auth Token:         %s
  OpenCode binary:    %s
`, profile, model, databricksHost, openaiBaseURL, redacted, opencodePath)
}
