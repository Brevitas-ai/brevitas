package cli

import (
	"context"
	"flag"
	"fmt"
	"text/tabwriter"

	"github.com/Brevitas-ai/brevitas/internal/provider"
)

func (a *App) cmdProviders(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("providers", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	detectedOnly := fs.Bool("detected", false, "only show tools detected on this machine")
	if err := fs.Parse(args); err != nil {
		return err
	}

	statuses := a.registry().Statuses(ctx)

	tw := tabwriter.NewWriter(a.Out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tSUPPORT\tSTATE\tNOTES")
	for _, s := range statuses {
		if *detectedOnly && !s.Detected {
			continue
		}
		notes := s.Reason
		if len(notes) > 60 {
			notes = notes[:57] + "…"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Support, stateLabel(s), notes)
	}
	return tw.Flush()
}

func stateLabel(s provider.Status) string {
	if !s.Detected {
		return "not detected"
	}
	return string(s.State)
}
