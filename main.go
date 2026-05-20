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
	"github.com/IceRhymers/databricks-opencode/internal/cmd"
	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	// completion <shell> — must be the very first check, before any flag parsing,
	// auth, or state loading. Safe to call in the Homebrew install sandbox.
	if len(os.Args) >= 2 && os.Args[1] == "completion" {
		completion.Run(os.Args[2:], flagDefs, "databricks-opencode", knownSubcommands...)
		os.Exit(0)
	}

	// `config` subcommand — persistent-config editor. Today this is just
	// `config show` (the lifted --print-env diagnostic); future sub-issues
	// may grow this tree. Routed before parseArgs so positional dispatch
	// doesn't have to fight with the transparent-passthrough behaviour.
	if len(os.Args) >= 2 && os.Args[1] == "config" {
		runConfigCommand(os.Args[2:])
		return
	}

	// `hooks` subcommand — opencode plugin lifecycle. Replaces the removed
	// --install-hooks / --uninstall-hooks / --headless-ensure root flags.
	// Routed before parseArgs so positional dispatch doesn't have to fight
	// with the transparent-passthrough behaviour.
	if len(os.Args) >= 2 && os.Args[1] == "hooks" {
		runHooksCommand(os.Args[2:])
		return
	}

	// `serve` subcommand — start the proxy without launching opencode.
	// Replaces the removed --headless / --idle-timeout root flags. Routed
	// before parseArgs so positional dispatch doesn't fight the
	// transparent-passthrough behaviour. The dispatcher in serve_cmd.go
	// parses its own flags then calls runOpencode with Headless=true.
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		runServeCommand(os.Args[2:])
		return
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

	a, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "databricks-opencode:", err)
		os.Exit(1)
	}

	runOpencode(a)
}

// runOpencode is the post-parse body of the wrapper-mode flow. Lifted out of
// main() in #84 so the `serve` dispatcher can synthesise an *Args (with
// Headless=true and IdleTimeout populated from `serve --idle-timeout`) and
// reuse the same proxy-startup + config-patch + opencode-launch logic.
//
// Wrapper invocation: main() builds *a from os.Args via parseArgs, leaving
// Headless=false and IdleTimeout=0 — runOpencode then launches opencode as a
// child after starting the proxy. Serve invocation: serve_cmd.runServeCommand
// builds *a from the post-`serve` arg list, sets Headless=true plus the
// resolved IdleTimeout, and runOpencode skips the opencode child launch and
// blocks on the lifecycle wrapper instead. The two paths share everything
// else (port binding, EnsureConfig, refcount, takeover, etc.) so the AC
// "byte-identical behaviour to --headless" holds by construction.
func runOpencode(a *Args) {
	verbose := a.Verbose
	version := a.Version
	showHelp := a.ShowHelp
	model := a.Model
	upstream := a.Upstream
	logFile := a.LogFile
	profile := a.Profile
	proxyAPIKey := a.ProxyAPIKey
	tlsCert := a.TLSCert
	tlsKey := a.TLSKey
	portFlag := a.Port
	headless := a.Headless
	idleTimeout := a.IdleTimeout
	noUpdateCheck := a.NoUpdateCheck
	opencodeArgs := a.OpencodeArgs

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
	if err := authcheck.EnsureAuthenticated(profile, ""); err != nil {
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
	if _, err := tp.Token(context.Background()); err != nil {
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
		gatewayURL = ConstructGatewayURL(host)
	}
	log.Printf("databricks-opencode: gateway URL: %s", gatewayURL)

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
	proxyHandler, err := NewProxyServer(&ProxyConfig{
		InferenceUpstream: gatewayURL,
		TokenProvider:     tp,
		Verbose:           verbose,
		APIKey:            proxyAPIKey,
		TLSCertFile:       tlsCert,
		TLSKeyFile:        tlsKey,
	})
	if err != nil {
		log.Fatalf("databricks-opencode: failed to create proxy server: %v", err)
	}

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
	// promoteCh, when non-nil, lets a non-owner be promoted to owner via the
	// health watcher's onTakeover callback (so /shutdown can fire correctly
	// after a takeover).
	var doneCh chan struct{}
	var promoteCh chan struct{}
	if headless {
		doneCh = make(chan struct{})
		promoteCh = make(chan struct{})
		proxyHandler = lifecycle.WrapWithLifecycle(lifecycle.Config{
			Inner:        proxyHandler,
			RefcountPath: "",
			IsOwner:      isOwner,
			PromoteCh:    promoteCh,
			IdleTimeout:  idleTimeout,
			APIKey:       proxyAPIKey,
			DoneCh:       doneCh,
			LogPrefix:    "databricks-opencode",
		})
	}

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
		// onTakeover closes promoteCh so the lifecycle wrapper promotes this
		// process to owner, enabling /shutdown to trigger a clean shutdown.
		onTakeover := func() {
			if promoteCh != nil {
				close(promoteCh)
			}
		}
		go health.WatchProxy(port, proxyHandler, tlsCert, tlsKey, "databricks-opencode", onTakeover)
	}

	proxyScheme := "http"
	if tlsCert != "" && tlsKey != "" {
		proxyScheme = "https"
	}
	proxyAddr := fmt.Sprintf("%s://127.0.0.1:%d", proxyScheme, portbind.ListenerPort(listener, port))
	log.Printf("databricks-opencode: local proxy %s -> %s", proxyAddr, gatewayURL)


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

// Args holds all parsed databricks-opencode flags plus the residual opencode args.
//
// Headless and IdleTimeout are NOT populated by parseArgs as of #84 — the
// `--headless` / `--idle-timeout` root flags were removed and replaced by the
// `serve` subcommand. Both fields remain on the struct because runOpencode
// reads them: the serve dispatcher (serve_cmd.go) synthesises an Args with
// Headless=true and IdleTimeout populated from `serve --idle-timeout` before
// calling runOpencode, so the post-parse logic stays single-sourced.
type Args struct {
	Verbose       bool
	Version       bool
	ShowHelp      bool
	Model         string
	Upstream      string
	LogFile       string
	Profile       string
	ProxyAPIKey   string
	TLSCert       string
	TLSKey        string
	Port          int
	Headless      bool          // populated only by the `serve` dispatcher
	IdleTimeout   time.Duration // populated only by the `serve` dispatcher
	NoUpdateCheck bool
	OpencodeArgs  []string
}

// parseArgs separates databricks-opencode flags from opencode flags.
//
// As of #84, --headless and --idle-timeout are NOT recognised at the root —
// they live under the `serve` subcommand. parseArgs leaves Args.Headless
// false and Args.IdleTimeout zero; the `serve` dispatcher in serve_cmd.go
// populates them when invoking runOpencode. Anything that looks like
// --headless / --idle-timeout at the root falls through to opencode (the
// transparent-passthrough behaviour the wrapper applies to all unknown
// flags).
func parseArgs(args []string) (*Args, error) {
	a := &Args{}

	// knownFlags is defined at package level in completion_flags.go,
	// derived from flagDefs so completions and parsing stay in sync.

	i := 0
	for i < len(args) {
		arg := args[i]

		// Explicit separator: everything after "--" goes to opencode.
		if arg == "--" {
			a.OpencodeArgs = append(a.OpencodeArgs, args[i+1:]...)
			return a, nil
		}

		if arg == "-h" {
			a.ShowHelp = true
			i++
			continue
		}
		if arg == "-v" {
			a.Verbose = true
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
						a.Model = value
					} else if i+1 < len(args) {
						i++
						a.Model = args[i]
					}
				case "--upstream":
					if value != "" {
						a.Upstream = value
					} else if i+1 < len(args) {
						i++
						a.Upstream = args[i]
					}
				case "--log-file":
					if value != "" {
						a.LogFile = value
					} else if i+1 < len(args) {
						i++
						a.LogFile = args[i]
					}
				case "--profile":
					if value != "" {
						a.Profile = value
					} else if i+1 < len(args) {
						i++
						a.Profile = args[i]
					}
				case "--proxy-api-key":
					if value != "" {
						a.ProxyAPIKey = value
					} else if i+1 < len(args) {
						i++
						a.ProxyAPIKey = args[i]
					}
				case "--tls-cert":
					if value != "" {
						a.TLSCert = value
					} else if i+1 < len(args) {
						i++
						a.TLSCert = args[i]
					}
				case "--tls-key":
					if value != "" {
						a.TLSKey = value
					} else if i+1 < len(args) {
						i++
						a.TLSKey = args[i]
					}
				case "--port":
					if value != "" {
						a.Port, _ = strconv.Atoi(value)
					} else if i+1 < len(args) {
						i++
						a.Port, _ = strconv.Atoi(args[i])
					}
				case "--verbose":
					a.Verbose = true
				case "--version":
					a.Version = true
				case "--help":
					a.ShowHelp = true
				case "--no-update-check":
					a.NoUpdateCheck = true
				default:
					return nil, fmt.Errorf("internal: %s is a known flag but parseArgs has no case for it", name)
				}
				i++
				continue
			}
		}

		// Not a known flag — pass through to opencode.
		a.OpencodeArgs = append(a.OpencodeArgs, arg)
		i++
	}
	return a, nil
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
// The first half is rendered from the rootCommand registry (commands.go) so
// the help body, flag set, and completion scripts share one source of truth.
func handleHelp(upstreamBinary string) {
	_ = cmd.Render(os.Stdout, rootCommand, map[string]string{"Version": Version})

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
	_ = token // intentionally never printed; always redacted to a fixed sentinel
	redacted := "**** (redacted)"

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

