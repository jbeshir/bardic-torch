package domain

// RevertDocument returns the document of a revert to target, an earlier approved document of the entity whose
// current document is current (D10): target's, except that an alias hidden in current, or absent from current and
// hidden in target, stays hidden, so that an old document never re-publishes an alias a moderator has hidden since
// (fail closed), and that the preferences are current's. A link hidden since stays in the document: its hidden
// state is the link's, which no document writes.
func RevertDocument[D Document](target, current D) D {
	var out any = target
	switch t := any(target).(type) {
	case PieceDocument:
		t.Preferences = any(current).(PieceDocument).Preferences
		out = t
	case PersonDocument:
		c := any(current).(PersonDocument)
		hidden := map[string]bool{}
		for _, a := range c.Aliases {
			if !a.IsPublic {
				hidden[CollationKey(a.Name)] = true
			}
		}
		t.Aliases = mapList(t.Aliases, func(a DocPersonAlias) DocPersonAlias {
			a.IsPublic = a.IsPublic && !hidden[CollationKey(a.Name)]

			return a
		})
		t.Preferences = c.Preferences
		out = t
	}

	return out.(D)
}
