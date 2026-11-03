package domain

// LinkRowUpdate returns the shared fields (URL, title, description, platform, place type, collection and locator)
// a links row gets when an approval writes next, a link of the approved document (D17). row is the row with next's
// normalized URL, nil if there is none, and prev the link with the same key in the entity's current document, nil
// if the entity's current document has none (a newly attached link, or a new entity):
//   - no row: every field from next;
//   - a link of the current document: the row's fields, but for those whose value next changed from prev;
//   - a link newly attached to a row that exists: the row's fields, but for those the row has none of.
//
// So an edit of one entity never resets what another entity's editor set, unless it changed it. Roles are not
// fields of the row (attachments hold them), and the result has none.
func LinkRowUpdate(row, prev *DocLink, next DocLink) DocLink {
	if row == nil {
		next.Roles = nil

		return next
	}
	out := *row
	out.Roles = nil
	if prev != nil && next.URL != prev.URL {
		out.URL = next.URL
	}
	out.Title = rowField(row.Title, prev, func(l DocLink) *string { return l.Title }, next)
	out.Description = rowField(row.Description, prev, func(l DocLink) *string { return l.Description }, next)
	out.Platform = rowField(row.Platform, prev, func(l DocLink) *string { return l.Platform }, next)
	out.PlaceType = rowField(row.PlaceType, prev, func(l DocLink) *LinkPlaceType { return l.PlaceType }, next)
	out.CollectionID = rowField(row.CollectionID, prev, func(l DocLink) *int64 { return l.CollectionID }, next)
	out.Locator = rowField(row.Locator, prev, func(l DocLink) *string { return l.Locator }, next)

	return out
}

// rowField is a nullable field of a links row after writing next, the field being get of a link: next's value if
// next changed it from prev, or, with no prev, if the row has none; the row's value otherwise.
func rowField[T comparable](row *T, prev *DocLink, get func(DocLink) *T, next DocLink) *T {
	if prev != nil && !equalPointers(get(*prev), get(next)) || prev == nil && row == nil {
		return get(next)
	}

	return row
}
