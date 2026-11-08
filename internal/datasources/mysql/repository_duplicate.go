package mysql

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.DuplicateFinder   = (*Repository)(nil)
	_ datasources.HiddenAliasLister = (*Repository)(nil)
)

// maxDuplicateNames is the most names of one kind the duplicate check expects to compare; each name query reads one
// more row than it, so that reaching it is noticed and the design revisited (D13).
const maxDuplicateNames = 20000

// FindDuplicates returns the existing entities the candidate may duplicate (D13), comparing its names with the whole
// name set of its entity type, in Go, by domain.DuplicateKey: committed rows only, outside any write transaction.
func (r *Repository) FindDuplicates(
	ctx context.Context, candidate domain.DuplicateCandidate, includeHidden bool,
) ([]domain.DuplicateMatch, error) {
	var matches []domain.DuplicateMatch
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		matches = c.findDuplicates(ctx, candidate, includeHidden)
		return c.err
	})
	if err != nil {
		return nil, fmt.Errorf("finding duplicates of a new %s: %w", candidate.EntityType, err)
	}

	return matches, nil
}

// findDuplicates returns the existing entities the candidate may duplicate (FindDuplicates).
func (c *catalogueReader) findDuplicates(
	ctx context.Context, candidate domain.DuplicateCandidate, includeHidden bool,
) []domain.DuplicateMatch {
	names := c.duplicateNames(ctx, candidate, includeHidden)
	if includeHidden {
		names = append(names, pendingDuplicateNames(candidate, c.pendingNewEntities(ctx, "", true))...)
	}

	return domain.FindDuplicates(candidate, names)
}

// duplicateNames returns the names of the public entities of the candidate's type.
func (c *catalogueReader) duplicateNames(
	ctx context.Context, candidate domain.DuplicateCandidate, includeHidden bool,
) []domain.DuplicateName {
	switch candidate.EntityType {
	case domain.EntityTypePiece:
		return c.pieceDuplicateNames(ctx)
	case domain.EntityTypePerson:
		return c.personDuplicateNames(ctx, includeHidden)
	case domain.EntityTypeTerm:
		return c.termDuplicateNames(ctx, candidate.Facet)
	case domain.EntityTypeCollection:
		return c.collectionDuplicateNames(ctx)
	case domain.EntityTypeEvent:
		return c.eventDuplicateNames(ctx)
	}

	return nil
}

// duplicateName is the name text of the entity match.
func duplicateName(match domain.DuplicateMatch, text string) domain.DuplicateName {
	return domain.DuplicateName{Match: match, Text: text}
}

// pieceDuplicateNames returns public pieces' titles and aliases.
func (c *catalogueReader) pieceDuplicateNames(ctx context.Context) []domain.DuplicateName {
	var names []domain.DuplicateName
	for _, row := range limitedNames(ctx, c, "piece titles", c.q.ListPieceTitles) {
		names = append(names, duplicateName(domain.DuplicateMatch{EntityType: domain.EntityTypePiece, Slug: row.Slug,
			Name: row.Title, Matched: domain.DuplicateFieldTitle}, row.Title))
	}
	for _, row := range limitedNames(ctx, c, "piece aliases", c.q.ListPieceAliasNames) {
		names = append(names, duplicateName(domain.DuplicateMatch{EntityType: domain.EntityTypePiece, Slug: row.Slug,
			Name: row.Title, Matched: domain.DuplicateFieldAlias}, row.Text))
	}

	return names
}

// personDuplicateNames returns public persons' names and public aliases, and with includeHidden their hidden
// aliases too; a match on a person who prefers no page is blocked. Without includeHidden, a person who prefers no
// page is compared by name only, never by an alias, so that a contributor cannot learn whose alias a name is
// (docs/PLAN.md §5.1 "Duplicate check").
func (c *catalogueReader) personDuplicateNames(ctx context.Context, includeHidden bool) []domain.DuplicateName {
	var names []domain.DuplicateName
	for _, row := range limitedNames(ctx, c, "person names", c.q.ListPersonNames) {
		names = append(names, duplicateName(domain.DuplicateMatch{EntityType: domain.EntityTypePerson, Slug: row.Slug,
			Name: row.Name, Matched: domain.DuplicateFieldName,
			Blocked: row.Status == queries.PersonsStatusPrefersNoPage}, row.Name))
	}
	aliases := func(ctx context.Context) ([]queries.ListPersonAliasNamesRow, error) {
		return c.q.ListPersonAliasNames(ctx, includeHidden)
	}
	for _, row := range limitedNames(ctx, c, "person aliases", aliases) {
		noPage := row.Status == queries.PersonsStatusPrefersNoPage
		if noPage && !includeHidden {
			continue
		}
		names = append(names, duplicateName(domain.DuplicateMatch{EntityType: domain.EntityTypePerson, Slug: row.Slug,
			Name: row.Name, Matched: domain.DuplicateFieldAlias, Blocked: noPage}, row.Alias))
	}

	return names
}

// termDuplicateNames returns the names and display names of the public terms of facet.
func (c *catalogueReader) termDuplicateNames(ctx context.Context, facet domain.Facet) []domain.DuplicateName {
	terms := func(ctx context.Context) ([]queries.ListFacetTermNamesRow, error) {
		return c.q.ListFacetTermNames(ctx, queries.TermsFacet(facet))
	}
	var names []domain.DuplicateName
	for _, row := range limitedNames(ctx, c, "term names", terms) {
		match := domain.DuplicateMatch{EntityType: domain.EntityTypeTerm, Slug: row.Slug, Name: row.DisplayName,
			Matched: domain.DuplicateFieldName}
		names = append(names, duplicateName(match, row.Name), duplicateName(match, row.DisplayName))
	}

	return names
}

// collectionDuplicateNames returns public collections' names and URLs.
func (c *catalogueReader) collectionDuplicateNames(ctx context.Context) []domain.DuplicateName {
	var names []domain.DuplicateName
	for _, row := range limitedNames(ctx, c, "collection names", c.q.ListCollectionNames) {
		match := domain.DuplicateMatch{EntityType: domain.EntityTypeCollection, CollectionID: c.signedID(row.ID),
			Name: row.Name, Matched: domain.DuplicateFieldName}
		names = append(names, duplicateName(match, row.Name))
		if row.Url.Valid {
			match.Matched = domain.DuplicateFieldURL
			names = append(names, duplicateName(match, row.Url.String))
		}
	}

	return names
}

// eventDuplicateNames returns public events' names.
func (c *catalogueReader) eventDuplicateNames(ctx context.Context) []domain.DuplicateName {
	var names []domain.DuplicateName
	for _, row := range limitedNames(ctx, c, "event names", c.q.ListEventNames) {
		names = append(names, duplicateName(domain.DuplicateMatch{EntityType: domain.EntityTypeEvent, Slug: row.Slug,
			Name: row.Name, Matched: domain.DuplicateFieldName}, row.Name))
	}

	return names
}

// limitedNames runs one name query of the duplicate check, logging a warning if it reached its limit: the names past
// it are not compared.
func limitedNames[T any](
	ctx context.Context, c *catalogueReader, what string, query func(context.Context) ([]T, error),
) []T {
	rows := fetchAll(ctx, c, what, query)
	if len(rows) > maxDuplicateNames {
		slog.WarnContext(ctx, "duplicate check reached its name limit; names past it are not compared",
			"names", what, "limit", maxDuplicateNames)
	}

	return rows
}

// fetchAll is fetch for a query without arguments.
func fetchAll[T any](ctx context.Context, c *catalogueReader, what string, query func(context.Context) (T, error)) T {
	return fetch(ctx, c, what, func(ctx context.Context, _ struct{}) (T, error) { return query(ctx) }, struct{}{})
}

// pendingDuplicateNames returns the names of the pending new entities of the candidate's type (of its facet, for
// a term), for moderator views: a piece's title and aliases, a person's name and aliases, a term's name and display
// name, a collection's or an event's name.
func pendingDuplicateNames(candidate domain.DuplicateCandidate, pending []pendingNewEntity) []domain.DuplicateName {
	entityType := candidate.EntityType
	var names []domain.DuplicateName
	for _, e := range pending {
		if term, ok := e.doc.(domain.TermDocument); e.entity.Type != entityType || ok && term.Facet != candidate.Facet {
			continue
		}
		match := domain.DuplicateMatch{EntityType: entityType, Slug: e.slug, Name: documentTitle(e.doc)}
		if entityType == domain.EntityTypeCollection {
			match.CollectionID = e.entity.ID
		}
		add := func(field domain.DuplicateField, texts ...string) {
			match.Matched = field
			for _, text := range texts {
				names = append(names, domain.DuplicateName{Match: match, Text: text})
			}
		}
		switch d := e.doc.(type) {
		case domain.PieceDocument:
			add(domain.DuplicateFieldTitle, d.Title)
			for _, a := range d.Aliases {
				add(domain.DuplicateFieldAlias, a.Text)
			}
		case domain.PersonDocument:
			add(domain.DuplicateFieldName, d.Name)
			for _, a := range d.Aliases {
				add(domain.DuplicateFieldAlias, a.Name)
			}
		case domain.TermDocument:
			add(domain.DuplicateFieldName, d.Name, d.DisplayName)
		default:
			add(domain.DuplicateFieldName, match.Name)
		}
	}

	return names
}

// ListHiddenAliasNames lists the names of every hidden alias.
func (r *Repository) ListHiddenAliasNames(ctx context.Context) ([]string, error) {
	names, err := r.queries.ListHiddenAliasNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing hidden alias names: %w", unavailableIfDisconnected(err))
	}

	return names, nil
}
