//go:build integration

package mysql_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

func TestRepository_ListLinksDueForCheck(t *testing.T) {
	db := mysqltest.NewDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 3, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for _, l := range []struct {
		id      int
		hidden  bool
		checked *time.Time
	}{
		{1, false, ptr(now.Add(-26 * day))}, {2, false, ptr(now.Add(-24 * day))}, {3, true, nil},
		{4, false, nil}, {5, false, ptr(now.Add(-60 * day))},
	} {
		_, err := db.ExecContext(ctx, `INSERT INTO links (id, url, normalized_url, is_hidden, last_checked_at)
			VALUES (?, ?, ?, ?, ?)`, l.id, "https://e.example/"+fmt.Sprint("link-", l.id), "e.example/"+fmt.Sprint("link-", l.id),
			l.hidden, l.checked)
		require.NoError(t, err)
	}

	links, err := mysql.New(db).ListLinksDueForCheck(ctx, now.Add(-25*day))
	require.NoError(t, err)
	// Never checked first, then longest unchecked; the recent and the hidden link are left out.
	var ids []uint64
	for _, l := range links {
		ids = append(ids, l.ID)
	}
	assert.Equal(t, []uint64{4, 5, 1}, ids)
}

func ptr[T any](v T) *T { return &v }
