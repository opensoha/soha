package sohacli

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

func runTUI(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("tui", args, rt)
	profile := fs.String("profile", "", "profile name")
	cluster := fs.String("cluster-id", "", "initial Soha cluster")
	namespace := fs.String("namespace", "default", "initial Kubernetes namespace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *cluster != "" && !kubernetesSegment(*cluster) || !kubernetesSegment(*namespace) {
		return usageError{message: "tui accepts only --profile, --cluster-id and --namespace"}
	}
	out, ok := rt.Out.(*os.File)
	if !terminalInput(rt) || !ok || !term.IsTerminal(int(out.Fd())) {
		return usageError{message: "tui requires an interactive input and output terminal; use native commands with --output json for automation"}
	}
	_, selectedProfile, profileConfig, err := loadRuntimeProfile(ctx, rt, *profile)
	if err != nil {
		return err
	}
	client := gatewayClient(rt, profileConfig)
	identity, err := client.AuthContext(ctx)
	if err != nil {
		return err
	}
	if err := validateAuthContext(identity); err != nil {
		return err
	}
	if rt.TerminalContext != nil {
		var stop context.CancelFunc
		ctx, stop = rt.TerminalContext(ctx)
		defer stop()
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	model := newTUIModel(ctx, tuiAPI{client: client}, selectedProfile, client.ServerURL, identity.SubjectID, *cluster, *namespace)
	model.loadContext = tuiRuntimeContextLoader(rt)
	defer model.detach()
	_, err = tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(rt.In), tea.WithOutput(rt.Out), tea.WithoutSignalHandler()).Run()
	referenceErr := model.writeRecoveryReferences(rt.Err)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("terminal interface: %w", err)
	}
	if referenceErr != nil {
		return fmt.Errorf("write original TUI request references: %w", referenceErr)
	}
	return nil
}

type tuiScope struct {
	tab, page                   int
	section, cluster, namespace string
	logs                        bool
	cursor                      string
}

type tuiRow struct {
	id, name, status, namespace string
	value                       map[string]any
}

type tuiSnapshot struct {
	rows       []tuiRow
	detail     any
	total      int64
	loaded     time.Time
	message    string
	nextCursor string
}

// Fixed application operations, shared by the model and deterministic tests.
// Implementations use the same authenticated clients as native commands.
type tuiBackend interface {
	load(context.Context, tuiScope, *tuiRow) (tuiSnapshot, error)
	act(context.Context, tuiScope, tuiRow, string) (any, error)
	chat(context.Context, string, string, string, func(aiStreamOutput) error) error
	follow(context.Context, string, string, int, func(aiRunStreamOutput) error) error
}
