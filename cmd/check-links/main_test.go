package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flags the cases give more than once.
const (
	limitFlag       = "--limit"
	maxDurationFlag = "--max-duration"
)

func TestParseOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want options
		// wantErr is whether the command line is refused with ErrUsage.
		wantErr bool
	}{
		{name: "defaults", want: options{limit: 400, archiveLimit: 200, maxDuration: 20 * time.Minute}},
		{name: "every flag", args: []string{limitFlag, "50", "--archive-limit=0", maxDurationFlag, "5m"},
			want: options{limit: 50, archiveLimit: 0, maxDuration: 5 * time.Minute}},
		{name: "zero limit", args: []string{limitFlag, "0"}, wantErr: true},
		{name: "limit too large", args: []string{limitFlag, "10001"}, wantErr: true},
		{name: "limit beyond int32", args: []string{limitFlag, "4294967296"}, wantErr: true},
		{name: "negative archive limit", args: []string{"--archive-limit", "-1"}, wantErr: true},
		{name: "duration too short", args: []string{maxDurationFlag, "30s"}, wantErr: true},
		{name: "duration too long", args: []string{maxDurationFlag, "7h"}, wantErr: true},
		{name: "unknown flag", args: []string{"--url", "x"}, wantErr: true},
		{name: "argument", args: []string{"now"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOptions(tc.args)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrUsage)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOptions_Request(t *testing.T) {
	t.Parallel()

	assert.Equal(t, command.CheckLinksRequest{
		Limit: 400, ArchiveLimit: 200, Workers: 4, MaxDuration: 20 * time.Minute, ArchiveSpacing: time.Second,
		LockPingInterval: time.Minute,
	}, options{limit: 400, archiveLimit: 200, maxDuration: 20 * time.Minute}.request())
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger, err := newLogger("", &out)
	require.NoError(t, err)
	logger.Debug("hidden")
	logger.Info("shown")
	assert.NotContains(t, out.String(), "hidden", "the default level is INFO")
	assert.Contains(t, out.String(), `"msg":"shown"`)

	_, err = newLogger("LOUD", &out)
	require.Error(t, err)
}

func TestRun_RefusesBadDatabaseURL(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), nil, "postgres://nowhere/db")
	require.Error(t, err)
	err = run(t.Context(), []string{limitFlag, "0"}, "")
	require.ErrorIs(t, err, ErrUsage)
}
