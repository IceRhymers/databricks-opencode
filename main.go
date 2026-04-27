package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IceRhymers/databricks-claude/pkg/authcheck"
	"github.com/IceRhymers/databricks-claude/pkg/completion"
	"github.com/IceRhymers/databricks-claude/pkg/health"
	"github.com/IceRhymers/databricks-claude/pkg/lifecycle"
	"github.com/IceRhymers/databricks-claude/pkg/portbind"
	"github.com/IceRhymers/databricks-claude/pkg/proxy"
	"github.com/IceRhymers/databricks-claude/pkg/refcount"
	"github.com/IceRhymers/databricks-claude/pkg/updater"
	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	// completion <shell> — must be the very first check, before any flag parsing,
	// auth, or state loading. Safe to call in the Homebrew install sandbox.
	if len(os.Args) >= 2 && os.Args[1] == "completion" {
		completion.Run(os.Args[2:], flagDefs, "databricks-opencode")
		os.Exit(0)
	}

	// update — force-check for a newer release and print instructions.
	if len(os.Args) >= 2 && os.Args[1] == "update" {
		if os.Getenv("DATABRICKS_NO_UPDATE_CHECK") == "1" {
			fmt.Fprintln(os.Stderr, "databricks-opencode: update check disabled via DATABRICKS_NO_UPDATE_CHECK")
			os.Exit(0)
		}
		cfg := buildUpdaterConfig()
		cfg.CacheTTL = 0 // force fresh check
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := updater.Check(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "databricks-opencode: update check failed: %v\n", err)
			os.Exit(1)
		}
		if !r.UpdateAvailable {
			fmt.Fprintf(os.Stderr, "databricks-opencode v%s is already the latest version\n", Version)
			os.Exit(0)
		}
		if r.IsHomebrew {
			fmt.Fprintf(os.Stderr, "Update available: v%s. Run: brew upgrade databricks-opencode\n", r.LatestVersion)
		} else {
			fmt.Fprintf(os.Stderr, "Update available: v%s. Download from: %s\n", r.LatestVersion, r.ReleaseURL)
		}
		os.Exit(0)
	}

	verbose, version, showHelp, printEnv, model, upstream, logFile, profile, proxyAPIKey, tlsCert, tlsKey, portFlag, headless, idleTimeout, installHooksFlag, uninstallHooksFlag, headlessEnsureFlag, noUpdateCheck, opencodeArgs := parseArgs(os.Args[1:])

	if showHelp {
		handleHelp(upstream)
		os.Exit(0)
	}

	if version {
		fmt.Printf("databricks-opencode %s\n", Version)
		os.Exit(0)
	}

	// --- Hook lifecycle commands (handled before auth/config setup) ---
	if installHooksFlag || uninstallHooksFlag {
		if installHooksFlag {
			if err := installHooks(); err != nil {
				log.Fatalf("databricks-opencode: --install-hooks: %v", err)
			}
			hookDir, _ := opencodeConfigDir()
			fmt.Fprintf(os.Stderr, "databricks-opencode: hooks installed — opencode plugin written to %s\n", filepath.Join(hookDir, "plugins", "databricks-proxy", "index.js"))
		} else {
			if err := uninstallHooks(); err != nil {
				log.Fatalf("databricks-opencode: --uninstall-hooks: %v", err)
			}
			fmt.Fprintln(os.Stderr, "databricks-opencode: hooks removed from opencode config")
		}
		os.Exit(0)
	}

	// --- Headless hook command (called by the opencode plugin, not by end users) ---
	if headlessEnsureFlag {
		state := loadState()
		port := resolvePort(0, state)
		headlessEnsure(port)
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
		model = "databricks-claude-opus-4-7"
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
	// Resolution chain: --port flag → saved state → defaultPort (49156).
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

	// --- Save TLS config to state so headless-ensure can use the right scheme ---
	{
		s := loadState()
		if s.TLSCert != tlsCert || s.TLSKey != tlsKey {
			s.TLSCert = tlsCert
			s.TLSKey = tlsKey
			if err := saveState(s); err != nil {
				log.Printf("databricks-opencode: failed to save TLS config: %v", err)
			}
		}
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
		go health.WatchProxy(port, proxyHandler, tlsCert, tlsKey, "databricks-opencode")
	}

	proxyScheme := "http"
	if tlsCert != "" && tlsKey != "" {
		proxyScheme = "https"
	}
	proxyAddr := fmt.Sprintf("%s://127.0.0.1:%d", proxyScheme, portbind.ListenerPort(listener, port))
	log.Printf("databricks-opencode: local proxy %s -> %s", proxyAddr, gatewayURL)

	// --- Reference counting (wrapper mode only) ---
	// In wrapper mode, the parent process acquires here and releases on exit.
	// In headless mode, refcount is not used — OpenCode has no exit hook to
	// release it, so the proxy relies on its idle timeout for shutdown.
	refcountPath := refcountPathForPort(port)
	if !headless {
		if err := refcount.Acquire(refcountPath); err != nil {
			log.Printf("databricks-opencode: refcount acquire warning: %v", err)
		}
	}

	// In headless mode, wrap handler with /shutdown endpoint and idle timeout.
	var doneCh chan struct{}
	if headless {
		doneCh = make(chan struct{})
		proxyHandler = lifecycle.WrapWithLifecycle(lifecycle.Config{
			Inner:        proxyHandler,
			RefcountPath: "",
			IsOwner:      isOwner,
			IdleTimeout:  idleTimeout,
			APIKey:       proxyAPIKey,
			DoneCh:       doneCh,
			LogPrefix:    "databricks-opencode",
		})
	}

	// --- Ensure config.json points at the local proxy (idempotent) ---
	// Use proxyAPIKey if explicitly set; otherwise use a fixed placeholder.
	// The proxy rewrites auth headers with a live Databricks token — the
	// value here just needs to be non-empty for the @ai-sdk/anthropic provider.
	configAPIKey := proxyAPIKey
	if configAPIKey == "" {
		configAPIKey = "databricks-proxy"
	}
	cfgDir, err := opencodeConfigDir()
	if err != nil {
		log.Fatalf("databricks-opencode: cannot determine opencode config dir: %v", err)
	}
	if err := EnsureConfig(jsonconfig.New(cfgDir), proxyAddr, model, configAPIKey, modelExplicit); err != nil {
		if headless {
			log.Printf("databricks-opencode: WARNING: failed to configure opencode: %v", err)
		} else {
			log.Fatalf("databricks-opencode: failed to configure opencode: %v", err)
		}
	}

	// --- Headless mode: print proxy URL and block until signal or shutdown ---
	if headless {
		runHeadless(proxyAddr, listener, isOwner, doneCh)
		return
	}

	// --- Synchronous update check (before child to avoid stderr interleaving) ---
	if !noUpdateCheck && os.Getenv("DATABRICKS_NO_UPDATE_CHECK") != "1" {
		updater.PrintUpdateNotice(buildUpdaterConfig())
	}

	log.Printf("databricks-opencode: launching opencode")

	// Inject MANAGED env var so the child opencode session's hooks don't
	// double-fire headlessEnsure.
	os.Setenv("DATABRICKS_OPENCODE_MANAGED", "1")

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
func parseArgs(args []string) (verbose bool, version bool, showHelp bool, printEnv bool, model string, upstream string, logFile string, profile string, proxyAPIKey string, tlsCert string, tlsKey string, port int, headless bool, idleTimeout time.Duration, installHooksFlag bool, uninstallHooksFlag bool, headlessEnsureFlag bool, noUpdateCheck bool, opencodeArgs []string) {
	idleTimeout = 30 * time.Minute // default

	// knownFlags is defined at package level in completion_flags.go,
	// derived from flagDefs so completions and parsing stay in sync.

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
				case "--idle-timeout":
					raw := value
					if raw == "" && i+1 < len(args) {
						i++
						raw = args[i]
					}
					if raw != "" {
						if d, err := time.ParseDuration(raw); err == nil {
							idleTimeout = d
						} else if mins, err := strconv.Atoi(raw); err == nil {
							idleTimeout = time.Duration(mins) * time.Minute
						}
					}
				case "--install-hooks":
					installHooksFlag = true
				case "--uninstall-hooks":
					uninstallHooksFlag = true
				case "--headless-ensure":
					headlessEnsureFlag = true
				case "--no-update-check":
					noUpdateCheck = true
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

// runHeadless runs the proxy without launching an opencode child process.
// It prints the proxy URL to stdout, then blocks until SIGINT/SIGTERM
// or until doneCh is closed (by /shutdown or idle timeout).
func runHeadless(proxyURL string, ln net.Listener, isOwner bool, doneCh chan struct{}) {
	fmt.Printf("PROXY_URL=%s\n", proxyURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		signal.Stop(sigCh)
	case <-doneCh:
		// Triggered by /shutdown or idle timeout.
	}

	if isOwner {
		ln.Close()
	}
}

// handleHelp prints the databricks-opencode help section, then execs opencode --help.
func handleHelp(upstreamBinary string) {
	fmt.Printf(`databricks-opencode v%s — Databricks AI Gateway wrapper for OpenCode CLI

Patches the opencode config (opencode.json) and runs a local proxy so the OpenCode CLI
authenticates through a Databricks AI Gateway endpoint with live token refresh.

Usage:
  databricks-opencode [databricks-opencode flags] [opencode flags] [opencode args]

Databricks-OpenCode Flags:
  --profile string      Databricks CLI profile (saved for future sessions; default: env or "DEFAULT")
  --upstream string     Override the AI Gateway URL (default: auto-discovered)
  --model string        Model to use (default: "databricks-claude-opus-4-7")
  --print-env           Print resolved configuration and exit (token redacted)
  --verbose, -v         Enable debug logging to stderr
  --log-file string     Write debug logs to a file (combinable with --verbose)
  --proxy-api-key string    Require this API key on all proxy requests (default: disabled)
  --tls-cert string         Path to TLS certificate file (requires --tls-key)
  --tls-key string          Path to TLS private key file (requires --tls-cert)
  --port int                Local proxy port (default: 49156, saved for future sessions)
  --headless            Start proxy without launching opencode (for IDE extensions)
  --idle-timeout duration   Idle timeout for headless mode (default 30m, 0 disables, bare number = minutes)
  --install-hooks       Install opencode plugin hooks for automatic proxy lifecycle
  --uninstall-hooks     Remove databricks-opencode plugin from opencode config
  --headless-ensure     Start proxy if not running (called by opencode plugin at init)
  --no-update-check            Skip the automatic update check on startup
  --version             Print version and exit
  --help, -h            Show this help message

Subcommands:
  completion <shell>           Generate shell completions (bash, zsh, fish)
  update                       Check for a newer release and print upgrade instructions

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

// buildUpdaterConfig returns the standard updater.Config for databricks-opencode.
func buildUpdaterConfig() updater.Config {
	cacheDir, _ := opencodeConfigDir()
	return updater.Config{
		RepoSlug:       "IceRhymers/databricks-opencode",
		CurrentVersion: Version,
		BinaryName:     "databricks-opencode",
		CacheFile:      filepath.Join(cacheDir, ".update-check.json"),
		CacheTTL:       24 * time.Hour,
	}
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

