package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	adaptersreg "github.com/infercore/infercore/internal/adapters"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/eval"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/replay"
	"github.com/infercore/infercore/internal/requests"
	"github.com/infercore/infercore/internal/retrieval"
	"github.com/infercore/infercore/internal/server"
	"github.com/spf13/cobra"
)

func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "serve")
	}
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var cfgPath string
	root := &cobra.Command{
		Use:   "infercore",
		Short: "InferCore AI request control plane",
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", envOr("INFERCORE_CONFIG", "configs/infercore.example.yaml"), "path to YAML config")

	root.AddCommand(serveCmd(&cfgPath))
	root.AddCommand(traceCmd(&cfgPath))
	root.AddCommand(replayCmd(&cfgPath))
	root.AddCommand(ledgerCmd(&cfgPath))
	root.AddCommand(evalCmd())
	return root
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func serveCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			srv := server.New(cfg)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(ctx)
			}()

			addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
			readTO, writeTO, idleTO := server.HTTPLayerTimeouts(cfg)
			httpServer := &http.Server{
				Addr:              addr,
				Handler:           srv.Routes(),
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       readTO,
				WriteTimeout:      writeTO,
				IdleTimeout:       idleTO,
			}
			go func() {
				log.Printf("infercore listening on %s", addr)
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatalf("server failed: %v", err)
				}
			}()
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			<-sig
			return httpServer.Shutdown(context.Background())
		},
	}
}

func traceCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "trace [request_id]",
		Short: "Print ledger record and steps as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			st, err := requests.NewFromConfig(cfg)
			if err != nil {
				return err
			}
			if st == nil {
				return fmt.Errorf("ledger is disabled; set ledger.enabled=true in config")
			}
			defer st.Close()
			ctx := context.Background()
			id := args[0]
			row, err := st.GetRequest(ctx, id)
			if err != nil {
				return err
			}
			steps, err := st.ListSteps(ctx, id)
			if err != nil {
				return err
			}
			out := map[string]any{
				"request": map[string]any{
					"request_id":       row.RequestID,
					"trace_id":         row.TraceID,
					"request_type":     row.RequestType,
					"tenant_id":        row.TenantID,
					"task_type":        row.TaskType,
					"priority":         row.Priority,
					"pipeline_ref":     row.PipelineRef,
					"input_json":       json.RawMessage(row.InputJSON),
					"context_json":     json.RawMessage(row.ContextJSON),
					"ai_request_json":  json.RawMessage(row.AIRequestJSON),
					"policy_snapshot":  json.RawMessage(row.PolicySnapshot),
					"status":           row.Status,
					"selected_backend": row.SelectedBackend,
					"route_reason":     row.RouteReason,
					"created_at":       row.CreatedAt,
					"updated_at":       row.UpdatedAt,
				},
				"steps": stepsToJSON(steps),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
}

func stepsToJSON(steps []requests.StepRow) []map[string]any {
	out := make([]map[string]any, 0, len(steps))
	for _, s := range steps {
		out = append(out, map[string]any{
			"request_id":    s.RequestID,
			"step_index":    s.StepIndex,
			"step_type":     s.StepType,
			"input_json":    json.RawMessage(s.InputJSON),
			"output_json":   json.RawMessage(s.OutputJSON),
			"backend":       s.Backend,
			"status":        s.Status,
			"error":         s.Error,
			"latency_ms":    s.LatencyMs,
			"metadata_json": json.RawMessage(s.MetadataJSON),
		})
	}
	return out
}

func replayCmd(cfgPath *string) *cobra.Command {
	var mode, idsFile string
	cmd := &cobra.Command{
		Use:   "replay [request_id ...]",
		Short: "Replay ledger request(s) via internal/replay (no HTTP API on gateway)",
		Long:  "With one request_id and no --ids-file, prints a single indented AIResponse JSON. With multiple IDs or --ids-file, prints one JSON object per line (NDJSON) with request_id, response or error.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var ids []string
			if strings.TrimSpace(idsFile) != "" {
				f, err := os.Open(idsFile)
				if err != nil {
					return err
				}
				defer f.Close()
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					ids = append(ids, line)
				}
				if err := sc.Err(); err != nil {
					return err
				}
			}
			ids = append(ids, args...)
			if len(ids) == 0 {
				return fmt.Errorf("provide at least one request_id or use --ids-file")
			}

			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			st, err := requests.NewFromConfig(cfg)
			if err != nil {
				return err
			}
			if st == nil {
				return fmt.Errorf("ledger is disabled; set ledger.enabled=true in config")
			}
			defer st.Close()
			adapterMap := buildAdapterMap(cfg)
			ret := retrieval.FromConfig(cfg)
			deps := replay.NewDependenciesFromConfig(cfg, adapterMap, ret)

			m := replay.Mode(strings.ToLower(strings.TrimSpace(mode)))
			if m != replay.ModeExact && m != replay.ModeCurrent {
				return fmt.Errorf("mode must be exact or current")
			}
			ctx := cmd.Context()
			if len(ids) == 1 && idsFile == "" {
				resp, err := replay.Replay(ctx, cfg, st, ids[0], m, deps)
				if err != nil {
					return err
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			enc := json.NewEncoder(os.Stdout)
			for _, id := range ids {
				resp, rerr := replay.Replay(ctx, cfg, st, id, m, deps)
				row := map[string]any{"request_id": id}
				if rerr != nil {
					row["error"] = rerr.Error()
				} else {
					row["response"] = resp
				}
				if err := enc.Encode(row); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "exact", "exact|current")
	cmd.Flags().StringVar(&idsFile, "ids-file", "", "one request_id per line (merged with positional IDs)")
	return cmd
}

func ledgerCmd(cfgPath *string) *cobra.Command {
	var idsFile, output string
	exportEval := &cobra.Command{
		Use:   "export-eval [request_id ...]",
		Short: "Build eval dataset JSON from ledger rows (uses ai_request_json)",
		Long:  "Loads each request_id from the configured ledger and writes a JSON array suitable for `infercore eval run --dataset`. Rows with selected_backend set include expected_backend for routing regression.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			st, err := requests.NewFromConfig(cfg)
			if err != nil {
				return err
			}
			if st == nil {
				return fmt.Errorf("ledger is disabled; set ledger.enabled=true in config")
			}
			defer st.Close()

			var ids []string
			if strings.TrimSpace(idsFile) != "" {
				f, err := os.Open(idsFile)
				if err != nil {
					return err
				}
				defer f.Close()
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					ids = append(ids, line)
				}
				if err := sc.Err(); err != nil {
					return err
				}
			}
			ids = append(ids, args...)
			if len(ids) == 0 {
				return fmt.Errorf("provide request_id arguments or --ids-file with one id per line")
			}

			ctx := context.Background()
			var items []eval.Item
			var convErrs []error
			for _, id := range ids {
				row, err := st.GetRequest(ctx, id)
				if err != nil {
					convErrs = append(convErrs, fmt.Errorf("%s: %w", id, err))
					continue
				}
				it, err := eval.ItemFromRequestRow(row)
				if err != nil {
					convErrs = append(convErrs, err)
					continue
				}
				items = append(items, it)
			}
			for _, e := range convErrs {
				_, _ = fmt.Fprintf(os.Stderr, "warn: %v\n", e)
			}
			if len(items) == 0 {
				return fmt.Errorf("no eval items exported (%d conversion/load errors)", len(convErrs))
			}

			var out *os.File = os.Stdout
			if strings.TrimSpace(output) != "" {
				out, err = os.Create(output)
				if err != nil {
					return err
				}
				defer out.Close()
			}
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(items)
		},
	}
	exportEval.Flags().StringVar(&idsFile, "ids-file", "", "file with one request_id per line (# comments and blank lines ignored)")
	exportEval.Flags().StringVarP(&output, "output", "o", "", "write JSON to this path (default: stdout)")

	root := &cobra.Command{Use: "ledger", Short: "Ledger utilities (export, etc.)"}
	root.AddCommand(exportEval)
	return root
}

func buildAdapterMap(cfg *config.Config) map[string]interfaces.BackendAdapter {
	out := make(map[string]interfaces.BackendAdapter)
	for _, b := range cfg.Backends {
		if a, ok := adaptersreg.NewBackend(b); ok {
			out[b.Name] = a
		}
	}
	return out
}

func evalCmd() *cobra.Command {
	var dataset, baseURL, pipeline, apiKey string
	run := &cobra.Command{
		Use:   "run",
		Short: "POST each dataset row to /infer and print summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(dataset) == "" {
				return fmt.Errorf("--dataset is required")
			}
			key := strings.TrimSpace(apiKey)
			if key == "" {
				key = strings.TrimSpace(os.Getenv("INFERCORE_API_KEY"))
			}
			ctx := context.Background()
			return eval.Run(ctx, baseURL, dataset, os.Stdout, pipeline, key)
		},
	}
	run.Flags().StringVar(&dataset, "dataset", "", "path to JSON array of eval items")
	run.Flags().StringVar(&baseURL, "base-url", "http://127.0.0.1:8080", "InferCore base URL")
	run.Flags().StringVar(&pipeline, "pipeline", "", "default pipeline_ref (e.g. inference/basic:v1)")
	run.Flags().StringVar(&apiKey, "api-key", "", "X-InferCore-Api-Key (optional; env INFERCORE_API_KEY if unset)")
	root := &cobra.Command{Use: "eval", Short: "Routing-focused evaluation"}
	root.AddCommand(run)
	return root
}
