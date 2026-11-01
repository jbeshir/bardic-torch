package mysql

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.PublicNeedsWorkLister = (*Repository)(nil)

// ListPublicNeedsWork lists one page of the public pieces and persons needing work, for reason or (if it is nil)
// any reason, pieces first, each by sort key and ID, with the total and the count for every reason, in two
// statements in one read-only transaction.
func (r *Repository) ListPublicNeedsWork(
	ctx context.Context, reason *domain.NeedsWorkReason, page domain.Page,
) (domain.NeedsWorkList, error) {
	items, err := needsWorkItemsStatement(reason, page)
	if err != nil {
		return domain.NeedsWorkList{}, fmt.Errorf("listing public needs-work entries: %w", err)
	}
	counts, err := needsWorkCountsStatement()
	if err != nil {
		return domain.NeedsWorkList{}, fmt.Errorf("listing public needs-work entries: %w", err)
	}

	var list domain.NeedsWorkList
	err = r.readOnly(ctx, func(c *catalogueReader) error {
		list.Entries = c.needsWorkEntries(ctx, items)
		list.Total, list.Counts = c.needsWorkCounts(ctx, counts, reason)
		return c.err
	})
	if err != nil {
		return domain.NeedsWorkList{}, fmt.Errorf("listing public needs-work entries: %w", err)
	}

	return list, nil
}

// needsWorkReasonColumn returns the candidates column saying whether a candidate needs work for reason.
func needsWorkReasonColumn(reason domain.NeedsWorkReason) (sqlFragment, error) {
	switch reason {
	case domain.NeedsWorkReasonStub:
		return "c.stub", nil
	case domain.NeedsWorkReasonUnassessed:
		return "c.unassessed", nil
	case domain.NeedsWorkReasonMissingLicensing:
		return "c.missing_licensing", nil
	case domain.NeedsWorkReasonDeadLinks:
		return "c.dead_links", nil
	default:
		return "", fmt.Errorf("%w [%s] of needs-work reason", errUnknownEnumValue, reason)
	}
}

// writeNeedsWorkWith writes the WITH clause defining candidates: every public piece (entity_order 0) and public
// person who does not prefer no page (entity_order 1), with a column per reason saying whether it needs work for
// it. A restricted piece needs work only for being a stub, and a dead link counts only if it is visible.
func writeNeedsWorkWith(s *statement) {
	s.write("WITH candidates AS (SELECT 0 AS entity_order, p.id, p.slug, p.title, p.sort_title AS sort_key, " +
		"p.is_stub AS stub, p.conservation_status = ")
	s.arg(string(domain.ConservationStatusUnassessed))
	s.write(" AS unassessed, p.conservation_status <> ")
	s.arg(string(domain.ConservationStatusRestricted))
	s.write(" AND p.licensing_status = ")
	s.arg(string(domain.LicensingStatusUnknown))
	s.write(" AS missing_licensing, p.conservation_status <> ")
	s.arg(string(domain.ConservationStatusRestricted))
	s.write(" AND EXISTS (SELECT 1 FROM link_attachments a JOIN links l ON l.id = a.link_id " +
		"WHERE a.piece_id = p.id AND NOT l.is_hidden AND l.liveness = ")
	s.arg(string(domain.LivenessDead))
	s.write(") AS dead_links FROM pieces p WHERE p.current_revision_id IS NOT NULL" +
		" UNION ALL SELECT 1, pe.id, pe.slug, pe.name, pe.sort_name, pe.is_stub, FALSE, pe.kind = ")
	s.arg(string(domain.PersonKindCommunityBard))
	s.write(" AND pe.licensing_status = ")
	s.arg(string(domain.LicensingStatusUnknown))
	s.write(", EXISTS (SELECT 1 FROM link_attachments a JOIN links l ON l.id = a.link_id " +
		"WHERE a.person_id = pe.id AND NOT l.is_hidden AND l.liveness = ")
	s.arg(string(domain.LivenessDead))
	s.write(") FROM persons pe WHERE pe.current_revision_id IS NOT NULL AND pe.status <> ")
	s.arg(string(domain.PersonStatusPrefersNoPage))
	s.write(")")
}

// needsWork is the condition that a candidate needs work for some reason.
const needsWork sqlFragment = "(c.stub OR c.unassessed OR c.missing_licensing OR c.dead_links)"

// writeReasonColumns writes, after a select list, a column for each reason in NeedsWorkReason's declaration
// order: the reason's candidates column, wrapped by column.
func writeReasonColumns(s *statement, column func(sqlFragment) []sqlFragment) error {
	for _, reason := range domain.NeedsWorkReasons() {
		name, err := needsWorkReasonColumn(reason)
		if err != nil {
			return err
		}
		s.write(", ")
		s.write(column(name)...)
	}

	return nil
}

// needsWorkItemsStatement selects the page of entries needing work for reason, or any reason if it is nil:
// (entity_order, slug, title, then whether it needs work for each reason), pieces first, by sort key and ID.
func needsWorkItemsStatement(reason *domain.NeedsWorkReason, page domain.Page) (*statement, error) {
	s := &statement{}
	writeNeedsWorkWith(s)
	s.write(" SELECT ", executionTimeHint, " c.entity_order, c.slug, c.title")
	if err := writeReasonColumns(s, func(name sqlFragment) []sqlFragment { return []sqlFragment{name} }); err != nil {
		return nil, err
	}
	s.write(" FROM candidates c WHERE ", needsWork)
	if reason != nil {
		column, err := needsWorkReasonColumn(*reason)
		if err != nil {
			return nil, err
		}
		s.write(" AND ", column)
	}
	s.write(" ORDER BY c.entity_order, c.sort_key, c.id")
	writePage(s, page)

	return s, nil
}

// needsWorkCountsStatement selects, of the entries needing work, how many there are, then how many need work for
// each reason.
func needsWorkCountsStatement() (*statement, error) {
	s := &statement{}
	writeNeedsWorkWith(s)
	s.write(" SELECT ", executionTimeHint, " COUNT(*)")
	if err := writeReasonColumns(s, func(name sqlFragment) []sqlFragment {
		return []sqlFragment{"CAST(COALESCE(SUM(", name, "), 0) AS SIGNED)"}
	}); err != nil {
		return nil, err
	}
	s.write(" FROM candidates c WHERE ", needsWork)

	return s, nil
}

func (c *catalogueReader) needsWorkEntries(ctx context.Context, s *statement) []domain.NeedsWorkEntry {
	reasons := domain.NeedsWorkReasons()
	entries := []domain.NeedsWorkEntry{}
	c.query(ctx, "needs-work entries", s, func(scan func(dest ...any) error) error {
		var (
			order int
			entry domain.NeedsWorkEntry
		)
		flags := make([]bool, len(reasons))
		dest := []any{&order, &entry.Slug, &entry.Title}
		for i := range flags {
			dest = append(dest, &flags[i])
		}
		if err := scan(dest...); err != nil {
			return err
		}
		entry.EntityType = domain.EntityTypePiece
		if order == 1 {
			entry.EntityType = domain.EntityTypePerson
		}
		for i, reason := range reasons {
			if flags[i] {
				entry.Reasons = append(entry.Reasons, reason)
			}
		}
		entries = append(entries, entry)
		return nil
	})

	return entries
}

// needsWorkCounts runs s, a needsWorkCountsStatement, and returns how many entries need work for reason, or any
// reason if it is nil, and the count for every reason.
func (c *catalogueReader) needsWorkCounts(
	ctx context.Context, s *statement, reason *domain.NeedsWorkReason,
) (int64, []domain.NeedsWorkCount) {
	reasons := domain.NeedsWorkReasons()
	var all int64
	byReason := make([]int64, len(reasons))
	c.query(ctx, "needs-work counts", s, func(scan func(dest ...any) error) error {
		dest := []any{&all}
		for i := range byReason {
			dest = append(dest, &byReason[i])
		}
		return scan(dest...)
	})

	total := all
	counts := make([]domain.NeedsWorkCount, 0, len(reasons))
	for i, r := range reasons {
		counts = append(counts, domain.NeedsWorkCount{Reason: r, Count: byReason[i]})
		if reason != nil && *reason == r {
			total = byReason[i]
		}
	}

	return total, counts
}
