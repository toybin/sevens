package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/repl"
)

func replCmd() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:               "repl [node-title]",
		Short:             "Start an interactive REPL session",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			// Ensure DB is closed on SIGINT/SIGTERM so the lock is released.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				stack.Close()
				os.Exit(0)
			}()

			globalCfg, err := config.LoadGlobalConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "[warn] loading global config: %v\n", err)
				// Non-fatal: continue with empty config.
			}

			// Optional initial focus from argument or active session.
			focusNode := ""
			if len(args) > 0 {
				focusNode = args[0]
			} else {
				session, _ := stack.KB.LoadCurrentSession(context.Background())
				if session != nil && session.Root == resolved {
					focusNode = session.Focus
					fmt.Fprintf(os.Stderr, "[session] resuming focus: %s\n", focusNode)
				}
			}

			proj := openProjection(stack)
			ps := function.NewPipelineStore(stack.Store)
			gq := newGraphQuerier(stack.KB, proj)
			ar := newApplyRunner(stack.KB, proj)
			tr := newTemplateRunner(stack.KB, proj)
			pr := newPipelineRunner(stack.KB, proj, ps, globalCfg)
			dr := newDiscussionRunner(stack.KB, proj, ps, globalCfg)

			r, err := repl.New(resolved, focusNode, globalCfg,
				repl.WithKB(stack.KB),
				repl.WithGraphQuerier(gq),
				repl.WithApplyRunner(ar),
				repl.WithTemplateRunner(tr),
				repl.WithPipelineRunner(pr),
				repl.WithDiscussionRunner(dr),
			)
			if err != nil {
				return fmt.Errorf("starting repl: %w", err)
			}
			return r.Run()
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory (defaults to cwd)")
	return cmd
}
