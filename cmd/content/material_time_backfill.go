package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/overmindv/content/internal/pkg/store/postgres"
	"github.com/overmindv/content/pkg/api/activity"
)

func runMaterialTimeBackfill(ctx context.Context, args []string, dsn, topic string, stdout, stderr io.Writer) int {
	opts, err := materialTimeOptions(args, topic, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)

		return 2
	}
	if strings.TrimSpace(dsn) == "" {
		_, _ = fmt.Fprintln(stderr, "DATABASE_URL is required")

		return 2
	}
	connect, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, err := pgx.Connect(connect, dsn)
	cancel()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "cannot connect to Content PostgreSQL")

		return 1
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	report, err := postgres.BackfillMaterialTimes(ctx, conn, opts)
	result := struct {
		Mode string `json:"mode"`
		postgres.MaterialTimeReport
		NextAfter string `json:"next_after,omitempty"`
	}{Mode: "report", MaterialTimeReport: report}
	if opts.Apply {
		result.Mode = "apply"
	}
	if report.NextAfter != nil {
		result.NextAfter = fmt.Sprintf("%s:%d", report.NextAfter.MaterialID, report.NextAfter.Version)
	}
	if encodeErr := json.NewEncoder(stdout).Encode(result); encodeErr != nil {
		_, _ = fmt.Fprintln(stderr, "cannot write the backfill report")

		return 1
	}
	if err != nil {
		var dbErr *pgconn.PgError
		if errors.As(err, &dbErr) {
			_, _ = fmt.Fprintf(stderr, "material time batch failed (SQLSTATE %s); resume from next_after\n", dbErr.Code)
		} else {
			_, _ = fmt.Fprintln(stderr, "material time batch interrupted or failed; resume from next_after")
		}

		return 1
	}

	return 0
}

func materialTimeOptions(args []string, topic string, stderr io.Writer) (postgres.MaterialTimeOptions, error) {
	var opts postgres.MaterialTimeOptions
	var after string
	if topic == "" {
		topic = activity.DefaultMaterialTopic
	}
	flags := flag.NewFlagSet("backfill-material-times", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.Topic, "topic", topic, "authoritative material outbox topic")
	flags.StringVar(&after, "after", "", "resume cursor from next_after")
	flags.IntVar(&opts.BatchSize, "batch-size", 25, "maximum snapshot rows per transaction (1-50)")
	flags.IntVar(&opts.Batches, "batches", 16, "maximum transactions (1-128)")
	flags.IntVar(&opts.EventLimit, "event-limit", 128, "maximum retained events per material (1-512)")
	flags.BoolVar(&opts.Apply, "apply", false, "write proven dates; otherwise run a read-only report")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, errors.New("unexpected positional argument")
	}
	if after != "" {
		id, version, ok := strings.Cut(after, ":")
		materialID, idErr := uuid.Parse(id)
		n, versionErr := strconv.ParseInt(version, 10, 64)
		if !ok || idErr != nil || id != materialID.String() || versionErr != nil || n < 0 {
			return opts, errors.New("after must be a canonical material UUID followed by :version")
		}
		opts.After = postgres.MaterialTimeCursor{MaterialID: materialID, Version: n}
	}

	return opts, opts.Validate()
}
