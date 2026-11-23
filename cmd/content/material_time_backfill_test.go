package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/overmindv/content/pkg/api/activity"
)

func TestMaterialTimeOptions(t *testing.T) {
	t.Parallel()

	opts, err := materialTimeOptions(nil, "", io.Discard)
	if err != nil || opts.Apply || opts.Topic != activity.DefaultMaterialTopic {
		t.Fatal("unexpected default options", opts, err)
	}
	const id = "33333333-3333-4333-8333-333333333333"
	opts, err = materialTimeOptions([]string{"--apply", "--after=" + id + ":3", "--batches=1"}, "catalog.material", io.Discard)
	if err != nil || !opts.Apply || opts.After.MaterialID.String() != id || opts.After.Version != 3 ||
		opts.Batches != 1 || opts.Topic != "catalog.material" {
		t.Fatal("unexpected continuation options", opts, err)
	}
	for _, arg := range []string{
		"--after=" + id + ":-1", "--after=" + id, "--batch-size=51", "--event-limit=513",
		"--batches=129", "--topic=x y", "unexpected",
	} {
		if _, err := materialTimeOptions([]string{arg}, "", io.Discard); err == nil {
			t.Error("accepted", arg)
		}
	}
}

func TestMaterialTimeErrorsDoNotPrintDSN(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	const secret = "private-credential"
	badDSN := "postgres://operator:" + secret + "@["
	code := runMaterialTimeBackfill(t.Context(), nil, badDSN, "", io.Discard, &stderr)
	if code != 1 || strings.Contains(stderr.String(), secret) {
		t.Fatal("connection error must be reported without credentials")
	}
}
