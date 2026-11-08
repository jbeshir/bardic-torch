package domain

// RevertDocument returns the document of a revert to target, an earlier approved document of the entity whose
// current document is current: target's, preferences included, except that an alias hidden in current, or absent
// from current and hidden in target, stays hidden, so that an old document never re-publishes an alias a moderator
// has hidden since (fail closed). A link hidden since stays in the document: its hidden state is the link's, which
// no document writes, and a preference whose source it is stays out of every view.
func RevertDocument[D Document](target, current D) D {
	t, ok := any(target).(PersonDocument)
	if !ok {
		return target
	}
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

	return any(t).(D)
}
