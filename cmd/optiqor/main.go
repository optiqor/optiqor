// Command optiqor is the open-source CLI entrypoint. Deterministic
// rule engine — no LLM, no telemetry by default. See ../../CLAUDE.md
// for the hard rules.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/optiqor/optiqor-cli/internal/analyze"
	"github.com/optiqor/optiqor-cli/internal/config"
	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/internal/render/style"
	roastpkg "github.com/optiqor/optiqor-cli/internal/roast"
	"github.com/optiqor/optiqor-cli/internal/share"
	"github.com/optiqor/optiqor-cli/pkg/htmlrender"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// Exit codes — stable contract for CI integration.
const (
	exitSuccess    = 0 // no findings ≥ threshold
	exitFindings   = 1 // findings ≥ threshold reported
	exitInvocation = 2 // invocation / parse error
	exitInternal   = 3 // unexpected runtime error
)

// errFindings is a sentinel returned from RunE so main can map it to exitFindings.
var errFindings = errors.New("optiqor: findings exceed threshold")

var version = "dev"

// accuracyDisclosure is the mandatory line every command's help and
// output must contain (hard rule per CLAUDE.md).
const accuracyDisclosure = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

func main() {
	err := newRootCmd().Execute()
	switch {
	case err == nil:
		os.Exit(exitSuccess)
	case errors.Is(err, errFindings):
		// Findings already rendered; suppress an extra error line.
		os.Exit(exitFindings)
	default:
		printError(os.Stderr, err)
		os.Exit(exitInvocation)
	}
}

func newRootCmd() *cobra.Command {
	var (
		noColor    bool
		configPath string
	)

	root := &cobra.Command{
		Use:   "optiqor",
		Short: "Cost analysis for Kubernetes Helm charts (security misconfigurations as a bonus)",
		Long: `optiqor analyzes Helm charts (or values files) for cost inefficiencies —
entirely offline, no login, no agent.

As a bonus, it also flags obvious security misconfigurations it spots while
parsing your chart (runAsRoot, :latest tags, missing securityContext, host
namespaces, etc.). Cost is the headline; security is a side-effect.

` + accuracyDisclosure,
		Example: `  # Analyze a Helm chart directory
  optiqor analyze ./my-chart

  # Analyze a single values file
  optiqor analyze ./values.yaml

  # Demo with a bundled chart
  optiqor demo

  # Pipe machine-readable JSON into jq
  optiqor analyze ./chart --json | jq .findings`,
		Version:           version,
		SilenceUsage:      true,
		SilenceErrors:     true, // we render errors ourselves
		DisableAutoGenTag: true,
	}

	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output (also: NO_COLOR env)")
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to .optiqor.yaml (default: ./.optiqor.yaml or $OPTIQOR_CONFIG)")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		// Config-level no_color implies --no-color.
		effectiveNoColor := noColor || cfg.NoColor
		ctx := cmd.Context()
		ctx = withColorPolicy(ctx, resolveColor(cmd, effectiveNoColor))
		ctx = withConfig(ctx, cfg)
		cmd.SetContext(ctx)
		return nil
	}

	root.SetVersionTemplate(versionTemplate())

	root.AddCommand(
		newAnalyzeCmd(),
		newDemoCmd(),
		newDiffCmd(),
		newScoreCmd(),
		newAuditCmd(),
		newWatchCmd(),
		newCompareCmd(),
	)

	return root
}

func versionTemplate() string {
	return fmt.Sprintf("optiqor %s — %s\n", version, "Helm chart cost analysis (security bonus)")
}

func newAnalyzeCmd() *cobra.Command {
	var (
		jsonOut     bool
		htmlPath    string
		offline     bool
		shareFlag   bool
		privateFlag bool
		roast       bool
		minSev      string
		detectors   []string
		failOn      string // severity threshold that triggers exit code 1
		outputPath  string
	)
	cmd := &cobra.Command{
		Use:   "analyze [chart]",
		Short: "Analyze a Helm chart or values file for cost findings (security as a bonus)",
		Long: `Reads a Helm chart directory or a single values file and reports cost
inefficiencies. Obvious security misconfigurations are flagged as a bonus
side-effect of parsing — they are not the headline feature.

` + accuracyDisclosure,
		Example: `  optiqor analyze ./my-chart
  optiqor analyze ./values.yaml --json
  optiqor analyze ./chart --severity=med --fail-on=high
  optiqor analyze ./chart --detector cpu-overprovisioned --detector missing-memory-limit
  optiqor analyze ./my-chart --roast    # same findings, snarkier titles
  optiqor analyze ./my-chart --share --private  # login-gated share link`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}

			if privateFlag && !shareFlag {
				return fmt.Errorf("--private requires --share: pass both flags together")
			}

			rep, err := analyze.RunPath(abs)
			if err != nil {
				return err
			}

			// Flags win over config when supplied.
			cfg := configFrom(cmd.Context())
			effSev := minSev
			if effSev == "" {
				effSev = cfg.MinSeverity
			}
			effDetectors := detectors
			if len(effDetectors) == 0 {
				effDetectors = cfg.Detectors
			}
			effFailOn := failOn
			if effFailOn == "" {
				effFailOn = cfg.FailOn
			}

			rep = analyze.Filter(rep, analyze.FilterOptions{
				MinSeverity: rules.Severity(toUpper(effSev)),
				DetectorIDs: effDetectors,
			})
			if roast {
				rep = roastpkg.Apply(rep)
			}
			if htmlPath != "" {
				if err := writeHTMLReport(htmlPath, rep); err != nil {
					return err
				}
				// --html is side-channel; stdout still gets text/JSON.
			}
			if err := emitReport(cmd, rep, jsonOut, outputPath, roast); err != nil {
				return err
			}
			if shareFlag {
				emitShareURL(cmd, rep, privateFlag)
			}
			_ = offline
			return checkFailOn(rep, effFailOn)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&htmlPath, "html", "", "also write a self-contained HTML report to this path")
	cmd.Flags().BoolVar(&offline, "offline", true, "do not perform any network calls (always true in Phase 1)")
	cmd.Flags().BoolVar(&shareFlag, "share", false, "print optiqor.dev/r/<hash> for the sanitised analysis (no upload in Phase 1)")
	cmd.Flags().BoolVar(&privateFlag, "private", false, "make the share link login-gated (requires --share; sends X-Optiqor-Private: 1)")
	cmd.Flags().BoolVar(&roast, "roast", false, "humorous output (findings stay accurate)")
	cmd.Flags().StringVar(&minSev, "severity", "", "drop findings below this severity (low|med|high)")
	cmd.Flags().StringArrayVar(&detectors, "detector", nil, "only run findings from these detector IDs (repeatable)")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit code 1 when any finding is at this severity or higher (low|med|high)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write the rendered output to a file instead of stdout")
	return cmd
}

// writeHTMLReport: pkg/htmlrender is also consumed by the backend's
// share-page handler so the local file and optiqor.dev/r/<hash>
// render byte-identically.
func writeHTMLReport(path string, rep render.Report) error {
	f, err := os.Create(path) //nolint:gosec // user-supplied output path
	if err != nil {
		return fmt.Errorf("open --html: %w", err)
	}
	defer func() { _ = f.Close() }()
	return htmlrender.Render(f, htmlrender.Data{
		Source:    rep.Source,
		Workloads: rep.Workloads,
		Findings:  rep.Findings,
		Mode:      htmlrender.ModeSandbox,
	})
}

// emitReport writes JSON or styled text. outputPath redirects to a
// file (CI use case); roast swaps tagline/footer — finding titles
// are roasted upstream in the analyze command.
func emitReport(cmd *cobra.Command, rep render.Report, jsonOut bool, outputPath string, roast bool) error {
	w, closeFn, err := openOutput(cmd, outputPath)
	if err != nil {
		return err
	}
	defer closeFn()
	if jsonOut {
		return render.JSON(w, rep)
	}
	opts := renderOpts(cmd)
	if roast {
		opts = renderOptsRoast(cmd)
	}
	return render.Text(w, rep, opts)
}

// openOutput returns stdout when path is empty, otherwise opens the
// file. The closer is a no-op for stdout so callers defer it
// unconditionally.
func openOutput(cmd *cobra.Command, path string) (io.Writer, func(), error) {
	if path == "" {
		return cmd.OutOrStdout(), func() {}, nil
	}
	f, err := os.Create(path) //nolint:gosec // user-specified output path
	if err != nil {
		return nil, nil, fmt.Errorf("open --output: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

// emitShareURL handles the `--share` flag end-to-end.
//
// It computes the local content-addressable hash, attempts to upload
// the sanitised payload to the sandbox endpoint, and prints the
// resulting `optiqor.dev/r/<hash>` URL to stderr (so JSON/text output on
// stdout stays clean).
//
// When private is true the upload request carries X-Optiqor-Private: 1
// so the sandbox receiver returns a token-gated URL (Phase 2 backend
// work). The CLI side only sets the header; the receiver handles the
// rest.
//
// The function never blocks the caller's success path — if the upload
// fails (offline, sandbox down, 5xx), we still print the URL so the
// user has a stable identifier they can re-share later. The endpoint
// is overridable via OPTIQOR_SHARE_URL for self-hosted deploys.

func emitShareURL(cmd *cobra.Command, rep any, private bool) {
	endpoint := os.Getenv("OPTIQOR_SHARE_URL")
	res := share.Upload(rep, endpoint, private)
	if res.Hash == "" {
		return
	}
	suffix := ""
	if res.Posted {
		if private {
			suffix = " (uploaded · login-gated)"
		} else {
			suffix = " (uploaded)"
		}
	} else if res.Error != "" {
		suffix = " (offline / not uploaded — " + res.Error + ")"
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "share URL: %s%s\n", res.URL, suffix)
}

// checkFailOn returns errFindings when any finding meets or exceeds
// threshold. Empty threshold is a no-op.
func checkFailOn(rep render.Report, threshold string) error {
	if threshold == "" {
		return nil
	}
	threshSev := rules.Severity(toUpper(threshold))
	if !validSeverity(threshSev) {
		return fmt.Errorf("invalid --fail-on severity %q (want low|med|high)", threshold)
	}
	for _, f := range rep.Findings {
		if severityRank(f.Severity) >= severityRank(threshSev) {
			return errFindings
		}
	}
	return nil
}

func severityRank(s rules.Severity) int {
	switch s {
	case rules.SeverityHigh:
		return 3
	case rules.SeverityMed:
		return 2
	case rules.SeverityLow:
		return 1
	}
	return 0
}

func validSeverity(s rules.Severity) bool {
	return s == rules.SeverityHigh || s == rules.SeverityMed || s == rules.SeverityLow
}

func toUpper(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// demoChart ships inside the binary so `npx @optiqor/cli demo` works
// with no input.
//
//go:embed demo/values.yaml
var demoChart []byte

func newDemoCmd() *cobra.Command {
	var (
		jsonOut bool
		roast   bool
	)
	cmd := &cobra.Command{
		Use:     "demo",
		Short:   "Run analysis on a bundled demo chart",
		Example: "  optiqor demo\n  optiqor demo --roast",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := analyze.Run(bytesReader(demoChart), analyze.Options{Source: "demo"})
			if err != nil {
				return err
			}
			if roast {
				rep = roastpkg.Apply(rep)
			}
			if jsonOut {
				return render.JSON(cmd.OutOrStdout(), rep)
			}
			opts := renderOpts(cmd)
			if roast {
				opts = renderOptsRoast(cmd)
			}
			return render.Text(cmd.OutOrStdout(), rep, opts)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().BoolVar(&roast, "roast", false, "humorous output (findings stay accurate)")
	return cmd
}

// renderOpts picks up the colour-policy decision stashed by the
// persistent pre-run.
func renderOpts(cmd *cobra.Command) render.Options {
	return render.Options{
		Color: colorPolicyFrom(cmd.Context()),
		Width: terminalWidth(),
	}
}

// renderOptsRoast supplies the playful strings so render stays
// unaware of internal/roast.
func renderOptsRoast(cmd *cobra.Command) render.Options {
	o := renderOpts(cmd)
	o.Roast = true
	o.RoastTagline = roastpkg.Tagline
	o.RoastFooter = roastpkg.FooterQuip
	return o
}

// resolveColor decides whether to emit ANSI. Precedence:
//
//  1. --no-color flag
//  2. NO_COLOR (per https://no-color.org)
//  3. CLICOLOR_FORCE=1 forces colour even when not a TTY
//  4. stdout is a TTY → on
//  5. otherwise → off
func resolveColor(cmd *cobra.Command, noColor bool) bool {
	if noColor {
		return false
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") == "1" {
		return true
	}
	out, ok := cmd.OutOrStdout().(*os.File)
	if !ok {
		return false
	}
	return style.IsTTY(out)
}

// terminalWidth returns $COLUMNS or 80.
func terminalWidth() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := atoi(v); err == nil && n > 20 {
			return n
		}
	}
	return 80
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// printError renders in red on a TTY; plain on a pipe.
func printError(w io.Writer, err error) {
	if err == nil {
		return
	}
	useColor := false
	if f, ok := w.(*os.File); ok {
		useColor = style.IsTTY(f) && os.Getenv("NO_COLOR") == ""
	}
	t := style.NewTheme(useColor)
	_, _ = fmt.Fprintln(w, t.SevHigh.Render(" ERROR ")+" "+err.Error())
}

// bytesReader avoids pulling bytes.NewReader into the main import graph.
func bytesReader(b []byte) *bytesReaderImpl { return &bytesReaderImpl{b: b} }

type bytesReaderImpl struct {
	b []byte
	i int
}

func (r *bytesReaderImpl) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func newDiffCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "diff <a> <b>",
		Short:   "Show cost delta between two values files",
		Args:    cobra.ExactArgs(2),
		Example: `  optiqor diff ./before/values.yaml ./after/values.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := analyze.DiffPaths(args[0], args[1])
			if err != nil {
				return err
			}
			if jsonOut {
				return rep.WriteJSON(cmd.OutOrStdout())
			}
			return rep.WriteText(cmd.OutOrStdout(), renderOpts(cmd))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	return cmd
}

func newScoreCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "score [chart]",
		Short:   "Assign an efficiency score to a chart",
		Args:    cobra.MaximumNArgs(1),
		Example: "  optiqor score ./my-chart\n  optiqor score ./values.yaml --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			rep, err := analyze.RunPath(abs)
			if err != nil {
				return err
			}
			s := analyze.Compute(rep.Source, rep.Workloads, rep.Findings)
			if jsonOut {
				return s.WriteJSON(cmd.OutOrStdout())
			}
			return s.WriteText(cmd.OutOrStdout(), renderOpts(cmd))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	return cmd
}

func newAuditCmd() *cobra.Command {
	var (
		jsonOut    bool
		failOn     string
		outputPath string
	)
	cmd := &cobra.Command{
		Use:     "audit [chart]",
		Short:   "Audit a chart for security findings only",
		Args:    cobra.MaximumNArgs(1),
		Example: "  optiqor audit ./my-chart\n  optiqor audit ./values.yaml --fail-on=high",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			rep, err := analyze.RunPath(abs)
			if err != nil {
				return err
			}
			rep = analyze.Filter(rep, analyze.FilterOptions{SecurityOnly: true})
			if err := emitReport(cmd, rep, jsonOut, outputPath, false); err != nil {
				return err
			}
			return checkFailOn(rep, failOn)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "exit code 1 when any finding is at this severity or higher (low|med|high)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write the rendered output to a file instead of stdout")
	return cmd
}

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch [chart]",
		Short: "Watch a chart and re-analyze on change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notYetImplemented(cmd)
		},
	}
}

func newCompareCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     "compare <a> <b>",
		Short:   "Side-by-side comparison of two charts (currently a diff alias)",
		Args:    cobra.ExactArgs(2),
		Example: `  optiqor compare ./before/values.yaml ./after/values.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := analyze.DiffPaths(args[0], args[1])
			if err != nil {
				return err
			}
			if jsonOut {
				return rep.WriteJSON(cmd.OutOrStdout())
			}
			return rep.WriteText(cmd.OutOrStdout(), renderOpts(cmd))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	return cmd
}

func notYetImplemented(cmd *cobra.Command) error {
	return fmt.Errorf("`optiqor %s` is not yet implemented (see https://optiqor.dev/roadmap)", cmd.Name())
}
