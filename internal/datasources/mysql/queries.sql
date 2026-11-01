-- ============================================
-- Users
-- ============================================

-- name: GetUser :one
SELECT id, display_name, role, created_at, blocked_at
FROM users
WHERE id = ?;

-- name: InsertUserIfMissing :exec
INSERT INTO users (id, display_name, role, created_at)
VALUES (?, ?, 'contributor', ?)
ON DUPLICATE KEY UPDATE id = id;

-- name: PromoteToModerator :exec
INSERT INTO users (id, display_name, role, created_at)
VALUES (?, ?, 'moderator', ?)
ON DUPLICATE KEY UPDATE role = 'moderator', blocked_at = NULL;

-- name: UpdateDisplayName :execrows
UPDATE users
SET display_name = ?
WHERE id = ?;

-- name: BlockUser :execrows
UPDATE users
SET blocked_at = COALESCE(blocked_at, sqlc.arg(blocked_at))
WHERE id = sqlc.arg(id) AND role = 'contributor';

-- name: UnblockUser :execrows
UPDATE users
SET blocked_at = NULL
WHERE id = ? AND role = 'contributor';

-- ============================================
-- API tokens
-- ============================================

-- name: CreateAPIToken :exec
INSERT INTO api_tokens (id, user_id, token_hash, token_prefix, name, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetAPITokenByHash :one
SELECT id, user_id, token_hash, token_prefix, name, created_at, last_used_at, revoked_at
FROM api_tokens
WHERE token_hash = ?;

-- name: UpdateAPITokenLastUsed :exec
UPDATE api_tokens
SET last_used_at = sqlc.arg(used_at)
WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < sqlc.arg(stale_before));

-- name: ListUserActiveAPITokens :many
SELECT id, user_id, token_hash, token_prefix, name, created_at, last_used_at, revoked_at
FROM api_tokens
WHERE user_id = ? AND revoked_at IS NULL
ORDER BY created_at DESC, id;

-- name: CountUserActiveAPITokens :one
SELECT COUNT(*)
FROM api_tokens
WHERE user_id = ? AND revoked_at IS NULL;

-- name: RevokeAPIToken :execrows
UPDATE api_tokens
SET revoked_at = COALESCE(revoked_at, sqlc.arg(revoked_at))
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- ============================================
-- Catalogue: public reads
--
-- An entity is public when it has an approved revision (current_revision_id IS NOT NULL); a term also needs
-- every ancestor approved (the public_terms view). Persons who prefer no page are dropped from every list that
-- references them (the listed_persons view), and hidden links from every read. What a link records is shown on
-- another entity's page only if the link has a public attachment (the public_link_attachments view).
-- ============================================

-- name: GetPublicPiece :one
SELECT id, slug, title, kind, conservation_status, is_stub, notes_md, version_notes_md, licensing_status,
    licensing_notes_md, licence, historical_source_md, period_start, period_end
FROM pieces
WHERE slug = ? AND current_revision_id IS NOT NULL;

-- name: ListPieceSecondaryKinds :many
SELECT p.slug, k.kind
FROM piece_secondary_kinds k
JOIN pieces p ON p.id = k.piece_id
WHERE p.slug IN (sqlc.slice('slugs'))
ORDER BY p.slug, k.kind;

-- name: ListPieceAliases :many
SELECT text, kind
FROM piece_aliases
WHERE piece_id = ?
ORDER BY id;

-- name: ListPublicPieceTerms :many
SELECT t.slug, t.facet, t.display_name, pt.is_primary, pt.part
FROM piece_terms pt
JOIN public_terms t ON t.id = pt.term_id
WHERE pt.piece_id = ?
ORDER BY t.facet, pt.position, t.id;

-- name: ListPublicPieceCredits :many
SELECT c.role, c.attribution, c.certainty, c.part, c.note,
    p.slug AS person_slug, p.name AS person_name, p.kind AS person_kind, p.is_stub AS person_is_stub
FROM piece_credits c
LEFT JOIN listed_persons p ON p.id = c.person_id
WHERE c.piece_id = ? AND (c.person_id IS NULL OR p.id IS NOT NULL)
ORDER BY c.position, c.id;

-- name: ListPublicPieceRelationsTo :many
SELECT r.type, p.slug, p.title, p.kind, p.conservation_status, p.is_stub
FROM piece_relations r
JOIN pieces p ON p.id = r.to_piece_id
WHERE r.from_piece_id = ? AND p.current_revision_id IS NOT NULL
ORDER BY r.type, p.sort_title, p.id;

-- name: ListPublicPieceRelationsFrom :many
SELECT r.type, p.slug, p.title, p.kind, p.conservation_status, p.is_stub
FROM piece_relations r
JOIN pieces p ON p.id = r.from_piece_id
WHERE r.to_piece_id = ? AND p.current_revision_id IS NOT NULL
ORDER BY r.type, p.sort_title, p.id;

-- ListPublicPieceLinks and ListPublicPersonLinks differ only in the attachment column, so that each uses its
-- (entity, position) index: a merged query with an OR of the two columns is planned as a scan of links. They must
-- select the same columns, which personLinkRows' conversion of one row type to the other checks at compile time.

-- name: ListPublicPieceLinks :many
SELECT l.id, l.url, l.title, l.description, l.platform, l.place_type, c.id AS collection_id,
    c.name AS collection_name, l.locator, l.liveness, l.last_checked_at, l.archived_url, a.role
FROM link_attachments a
JOIN links l ON l.id = a.link_id
LEFT JOIN collections c ON c.id = l.collection_id AND c.current_revision_id IS NOT NULL
WHERE a.piece_id = ? AND NOT l.is_hidden
ORDER BY a.position, l.id, a.id;

-- name: ListPublicPiecePerformances :many
SELECT perf.id, perf.link_id, perf.year, perf.uploader, perf.description_verbatim,
    e.slug AS event_slug, e.name AS event_name, e.kind AS event_kind
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN events e ON e.id = perf.event_id AND e.current_revision_id IS NOT NULL
WHERE perf.piece_id = ? AND NOT l.is_hidden
    AND EXISTS (SELECT 1 FROM link_attachments a WHERE a.link_id = perf.link_id AND a.piece_id = perf.piece_id)
ORDER BY perf.link_id, perf.id;

-- name: ListPublicPerformers :many
SELECT pp.performance_id, p.slug, p.name, p.kind, p.is_stub
FROM performance_performers pp
JOIN listed_persons p ON p.id = pp.person_id
WHERE pp.performance_id IN (sqlc.slice('performance_ids'))
ORDER BY pp.performance_id, pp.position;

-- name: ListConservationHistory :many
SELECT status, reason, source_url, at
FROM conservation_history
WHERE piece_id = ?
ORDER BY at, id;

-- name: GetPublicPerson :one
SELECT id, slug, name, kind, status, status_date, status_source_url, is_stub, pronouns, notes_md, licensing_status,
    licensing_notes_md
FROM persons
WHERE slug = ? AND current_revision_id IS NOT NULL;

-- name: ListPublicPersonAliases :many
SELECT name, kind
FROM person_aliases
WHERE person_id = ? AND is_public
ORDER BY id;

-- name: ListPersonRoles :many
SELECT role
FROM person_roles
WHERE person_id = ?
ORDER BY role;

-- name: ListPublicPersonAffiliations :many
SELECT t.slug, t.facet, t.display_name
FROM person_affiliations pa
JOIN public_terms t ON t.id = pa.term_id
WHERE pa.person_id = ?
ORDER BY t.position, t.display_name, t.id;

-- name: ListPublicPersonRelationsTo :many
SELECT r.type, p.slug, p.name, p.kind, p.is_stub
FROM person_relations r
JOIN listed_persons p ON p.id = r.to_person_id
WHERE r.from_person_id = ?
ORDER BY r.type, p.sort_name, p.id;

-- name: ListPublicPersonRelationsFrom :many
SELECT r.type, p.slug, p.name, p.kind, p.is_stub
FROM person_relations r
JOIN listed_persons p ON p.id = r.from_person_id
WHERE r.to_person_id = ?
ORDER BY r.type, p.sort_name, p.id;

-- Identical to ListPublicPieceLinks but for the attachment column.

-- name: ListPublicPersonLinks :many
SELECT l.id, l.url, l.title, l.description, l.platform, l.place_type, c.id AS collection_id,
    c.name AS collection_name, l.locator, l.liveness, l.last_checked_at, l.archived_url, a.role
FROM link_attachments a
JOIN links l ON l.id = a.link_id
LEFT JOIN collections c ON c.id = l.collection_id AND c.current_revision_id IS NOT NULL
WHERE a.person_id = ? AND NOT l.is_hidden
ORDER BY a.position, l.id, a.id;

-- name: ListPublicPersonWorks :many
SELECT c.role, c.certainty, c.part, p.slug, p.title, p.kind, p.conservation_status, p.is_stub
FROM piece_credits c
JOIN pieces p ON p.id = c.piece_id
WHERE c.person_id = sqlc.arg(person_id) AND p.current_revision_id IS NOT NULL
ORDER BY p.sort_title, p.id, c.position, c.id
LIMIT ?;

-- name: CountPublicPersonWorks :one
SELECT COUNT(*)
FROM piece_credits c
JOIN pieces p ON p.id = c.piece_id
WHERE c.person_id = ? AND p.current_revision_id IS NOT NULL;

-- A person's performances are those recorded by links attached to them, and those they perform in on links with
-- a public attachment, as a performance only an unapproved or no-page entity's links record may name them. A
-- performance of a piece that is not public is left out entirely, as its description may name the piece.

-- name: ListPublicPersonPerformances :many
SELECT perf.year, perf.description_verbatim, l.url, l.title, l.platform,
    p.slug AS piece_slug, p.title AS piece_title, p.kind AS piece_kind,
    p.conservation_status AS piece_conservation_status, p.is_stub AS piece_is_stub,
    e.slug AS event_slug, e.name AS event_name, e.kind AS event_kind
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN pieces p ON p.id = perf.piece_id
LEFT JOIN events e ON e.id = perf.event_id AND e.current_revision_id IS NOT NULL
WHERE NOT l.is_hidden
    AND (perf.piece_id IS NULL OR p.current_revision_id IS NOT NULL)
    AND (
        (EXISTS (SELECT 1 FROM performance_performers pp
                WHERE pp.performance_id = perf.id AND pp.person_id = sqlc.arg(performer_id))
            AND EXISTS (SELECT 1 FROM public_link_attachments pa WHERE pa.link_id = perf.link_id))
        OR EXISTS (SELECT 1 FROM link_attachments a
            WHERE a.link_id = perf.link_id AND a.person_id = sqlc.arg(attached_person_id))
    )
ORDER BY perf.year IS NULL, perf.year DESC, perf.id
LIMIT ?;

-- name: CountPublicPersonPerformances :one
SELECT COUNT(*)
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN pieces p ON p.id = perf.piece_id
WHERE NOT l.is_hidden
    AND (perf.piece_id IS NULL OR p.current_revision_id IS NOT NULL)
    AND (
        (EXISTS (SELECT 1 FROM performance_performers pp
                WHERE pp.performance_id = perf.id AND pp.person_id = sqlc.arg(performer_id))
            AND EXISTS (SELECT 1 FROM public_link_attachments pa WHERE pa.link_id = perf.link_id))
        OR EXISTS (SELECT 1 FROM link_attachments a
            WHERE a.link_id = perf.link_id AND a.person_id = sqlc.arg(attached_person_id))
    );

-- name: ListPublicTerms :many
SELECT t.id, t.slug, t.facet, t.name, t.display_name, parent.slug AS parent_slug
FROM public_terms t
LEFT JOIN terms parent ON parent.id = t.parent_id
WHERE (sqlc.narg(facet) IS NULL OR t.facet = sqlc.narg(facet))
ORDER BY t.facet, t.position, t.display_name, t.id;

-- name: GetPublicTerm :one
SELECT t.id, t.slug, t.facet, t.name, t.display_name, t.description_md,
    ra.slug AS related_slug, ra.facet AS related_facet, ra.display_name AS related_display_name
FROM public_terms t
LEFT JOIN public_terms ra ON ra.id = t.related_affiliation_id
WHERE t.slug = ?;

-- A term's counts are of the distinct public pieces tagged with, and public persons with a page affiliated
-- with, the term or any public descendant: both ends of the closure are checked, so a pending term hides its
-- subtree from its ancestors' counts too. A term with no row of a kind has a count of 0 of it.

-- name: ListPublicTermCounts :many
SELECT ta.ancestor_id AS term_id, 'piece' AS member, COUNT(DISTINCT pt.piece_id) AS n
FROM term_ancestors ta
JOIN public_terms d ON d.id = ta.descendant_id
JOIN piece_terms pt ON pt.term_id = ta.descendant_id
JOIN pieces p ON p.id = pt.piece_id
WHERE ta.ancestor_id IN (sqlc.slice('piece_term_ids')) AND p.current_revision_id IS NOT NULL
GROUP BY ta.ancestor_id
UNION ALL
SELECT ta.ancestor_id, 'person', COUNT(DISTINCT pa.person_id)
FROM term_ancestors ta
JOIN public_terms d ON d.id = ta.descendant_id
JOIN person_affiliations pa ON pa.term_id = ta.descendant_id
JOIN listed_persons p ON p.id = pa.person_id
WHERE ta.ancestor_id IN (sqlc.slice('person_term_ids'))
GROUP BY ta.ancestor_id;

-- name: ListTermAliases :many
SELECT name
FROM term_aliases
WHERE term_id = ?
ORDER BY id;

-- A public term's ancestors are all public.

-- name: ListTermAncestors :many
SELECT anc.slug, anc.facet, anc.display_name
FROM term_ancestors ta
JOIN terms anc ON anc.id = ta.ancestor_id
WHERE ta.descendant_id = ? AND ta.depth > 0
ORDER BY ta.depth DESC;


-- name: ListPublicTermChildren :many
SELECT t.id, t.slug, t.facet, t.display_name
FROM public_terms t
WHERE t.parent_id = ?
ORDER BY t.position, t.display_name, t.id;

-- name: ListPublicTermPieces :many
SELECT p.slug, p.title, p.kind, p.conservation_status, p.is_stub
FROM pieces p
WHERE p.current_revision_id IS NOT NULL
    AND EXISTS (
        SELECT 1
        FROM piece_terms pt
        JOIN term_ancestors ta ON ta.descendant_id = pt.term_id
        JOIN public_terms d ON d.id = ta.descendant_id
        WHERE pt.piece_id = p.id AND ta.ancestor_id = sqlc.arg(term_id)
    )
ORDER BY p.sort_title, p.id
LIMIT ? OFFSET ?;

-- name: ListPublicTermPersons :many
SELECT p.slug, p.name, p.kind, p.is_stub
FROM listed_persons p
WHERE EXISTS (
        SELECT 1
        FROM person_affiliations pa
        JOIN term_ancestors ta ON ta.descendant_id = pa.term_id
        JOIN public_terms d ON d.id = ta.descendant_id
        WHERE pa.person_id = p.id AND ta.ancestor_id = sqlc.arg(term_id)
    )
ORDER BY p.sort_name, p.id
LIMIT ? OFFSET ?;

-- name: GetPublicCollection :one
SELECT c.id, c.name, c.kind, c.url, c.is_bardic_resource, c.description_md, c.enumerable,
    r.slug AS region_slug, r.facet AS region_facet, r.display_name AS region_display_name,
    parent.id AS parent_id, parent.name AS parent_name, parent.kind AS parent_kind, parent.url AS parent_url,
    parent.is_bardic_resource AS parent_is_bardic_resource
FROM collections c
LEFT JOIN public_terms r ON r.id = c.region_term_id
LEFT JOIN collections parent ON parent.id = c.parent_id AND parent.current_revision_id IS NOT NULL
WHERE c.id = ? AND c.current_revision_id IS NOT NULL;

-- name: ListPublicCollectionChildren :many
SELECT id, name, kind, url, is_bardic_resource
FROM collections
WHERE parent_id = ? AND current_revision_id IS NOT NULL
ORDER BY sort_name, id;

-- A collection's citations are its visible links, once per public piece or public person with a page that each
-- is attached to, whatever roles it is attached with.

-- name: ListPublicCollectionCitations :many
SELECT DISTINCT l.id AS link_id, l.url, l.title, l.locator, a.piece_id, a.person_id,
    COALESCE(p.sort_title, pe.sort_name) AS sort_key,
    p.slug AS piece_slug, p.title AS piece_title, p.kind AS piece_kind,
    p.conservation_status AS piece_conservation_status, p.is_stub AS piece_is_stub,
    pe.slug AS person_slug, pe.name AS person_name, pe.kind AS person_kind, pe.is_stub AS person_is_stub
FROM links l
JOIN public_link_attachments a ON a.link_id = l.id
LEFT JOIN pieces p ON p.id = a.piece_id
LEFT JOIN persons pe ON pe.id = a.person_id
WHERE l.collection_id = ? AND NOT l.is_hidden
ORDER BY sort_key, l.id, a.piece_id, a.person_id
LIMIT ?;

-- name: CountPublicCollectionCitations :one
SELECT COUNT(*)
FROM (
    SELECT DISTINCT l.id, a.piece_id, a.person_id
    FROM links l
    JOIN public_link_attachments a ON a.link_id = l.id
    WHERE l.collection_id = ? AND NOT l.is_hidden
) cited;

-- name: ListLinkAttachmentRoles :many
SELECT link_id, piece_id, person_id, role
FROM link_attachments
WHERE link_id IN (sqlc.slice('link_ids'))
ORDER BY link_id, position, id;

-- name: ListPublicEvents :many
SELECT slug, name, kind
FROM events
WHERE current_revision_id IS NOT NULL AND (sqlc.narg(kind) IS NULL OR kind = sqlc.narg(kind))
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountPublicEvents :one
SELECT COUNT(*)
FROM events
WHERE current_revision_id IS NOT NULL AND (sqlc.narg(kind) IS NULL OR kind = sqlc.narg(kind));

-- name: GetPublicEvent :one
SELECT id, slug, name, kind, description_md
FROM events
WHERE slug = ? AND current_revision_id IS NOT NULL;

-- An event's performances are on visible links attached to a public piece or a public person with a page, as
-- a performance only an unapproved or no-page entity's links record may name them. A performance of a piece
-- that is not public is left out entirely, as on a person's page.

-- name: ListPublicEventPerformances :many
SELECT perf.id, perf.year, perf.description_verbatim, l.url, l.title, l.platform,
    p.slug AS piece_slug, p.title AS piece_title, p.kind AS piece_kind,
    p.conservation_status AS piece_conservation_status, p.is_stub AS piece_is_stub
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN pieces p ON p.id = perf.piece_id
WHERE perf.event_id = sqlc.arg(event_id) AND NOT l.is_hidden
    AND (perf.piece_id IS NULL OR p.current_revision_id IS NOT NULL)
    AND EXISTS (SELECT 1 FROM public_link_attachments a WHERE a.link_id = perf.link_id)
ORDER BY perf.year IS NULL, perf.year DESC, perf.id
LIMIT ?;

-- name: CountPublicEventPerformances :one
SELECT COUNT(*)
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN pieces p ON p.id = perf.piece_id
WHERE perf.event_id = ? AND NOT l.is_hidden
    AND (perf.piece_id IS NULL OR p.current_revision_id IS NOT NULL)
    AND EXISTS (SELECT 1 FROM public_link_attachments a WHERE a.link_id = perf.link_id);

-- ============================================
-- Catalogue: public history and revisions
--
-- Only approved revisions of public entities are public. Authors are shown by display name only.
-- ============================================

-- name: GetPublicPieceID :one
SELECT id FROM pieces WHERE slug = ? AND current_revision_id IS NOT NULL;

-- name: GetPublicPersonID :one
SELECT id FROM persons WHERE slug = ? AND current_revision_id IS NOT NULL;

-- name: GetPublicTermID :one
SELECT t.id
FROM public_terms t
WHERE t.slug = ?;

-- name: GetPublicEventID :one
SELECT id FROM events WHERE slug = ? AND current_revision_id IS NOT NULL;

-- name: GetPublicCollectionID :one
SELECT id FROM collections WHERE id = ? AND current_revision_id IS NOT NULL;

-- name: ListApprovedRevisions :many
SELECT r.id, r.source, r.summary, r.created_at, u.display_name AS author_display_name
FROM revisions r
LEFT JOIN users u ON u.id = r.author_user_id
WHERE r.entity_type = ? AND r.entity_id = ? AND r.status = 'approved'
ORDER BY r.created_at DESC, r.id DESC
LIMIT ? OFFSET ?;

-- name: CountApprovedRevisions :one
SELECT COUNT(*)
FROM revisions
WHERE entity_type = ? AND entity_id = ? AND status = 'approved';

-- name: ListRevisionContributors :many
SELECT revision_id, name
FROM revision_contributors
WHERE revision_id IN (sqlc.slice('revision_ids'))
ORDER BY revision_id, position;

-- name: GetApprovedRevision :one
SELECT r.id, r.entity_type, r.entity_id, r.source, r.summary, r.created_at, r.snapshot_json,
    u.display_name AS author_display_name
FROM revisions r
LEFT JOIN users u ON u.id = r.author_user_id
WHERE r.id = ? AND r.status = 'approved';

-- A revision's base is shown only if it is an approved revision of the same entity.

-- name: GetApprovedBaseRevision :one
SELECT b.id, b.snapshot_json
FROM revisions r
JOIN revisions b ON b.id = r.base_revision_id
WHERE r.id = ? AND b.status = 'approved' AND b.entity_type = r.entity_type AND b.entity_id = r.entity_id;

-- The current state of the entities and links snapshot documents reference, which their views are redacted by.

-- name: ListPublicPiecesByID :many
SELECT id, slug, title, kind, conservation_status, is_stub
FROM pieces
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- name: ListPublicPersonsByID :many
SELECT id, slug, name, kind, status, is_stub
FROM persons
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- name: ListPublicAliasNames :many
SELECT person_id, name
FROM person_aliases
WHERE person_id IN (sqlc.slice('person_ids')) AND is_public;

-- name: ListPublicTermsByID :many
SELECT t.id, t.slug, t.facet, t.display_name
FROM public_terms t
WHERE t.id IN (sqlc.slice('ids'));

-- name: ListPublicCollectionsByID :many
SELECT id, name, kind, url, is_bardic_resource
FROM collections
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- name: ListPublicEventsByID :many
SELECT id, slug, name, kind
FROM events
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- A snapshot's link is shown only if its normalized URL is that of a link that is not hidden. Every link's
-- normalized_url is domain.NormalizeURL(url) (DEPLOY.md, "The normalized URL invariant"), which the caller applies
-- to the snapshot's URLs too, so a link edited to another spelling of the same URL is still matched, and a link
-- since deleted or given another URL fails closed. The column is binary, and the caller trusts only the normalized
-- URLs these rows hold.

-- name: ListLinksByNormalizedURL :many
SELECT normalized_url, is_hidden
FROM links
WHERE normalized_url IN (sqlc.slice('normalized_urls'));

-- ============================================
-- Catalogue import
--
-- The importer writes a whole catalogue into an empty database in one transaction, with the IDs the import
-- assigned, every row of a table before the rows of tables referring to it. Each entity row carries its approved
-- import revision as its current revision directly. A links row's normalized_url is domain.NormalizeURL(url)
-- (DEPLOY.md, "The normalized URL invariant"), and its liveness is the column default.
-- ============================================

-- name: InsertImportedRevision :exec
INSERT INTO revisions (id, entity_type, entity_id, source, status, snapshot_json, summary, created_at)
VALUES (?, ?, ?, 'import', 'approved', ?, ?, ?);

-- name: InsertRevisionContributor :exec
INSERT INTO revision_contributors (revision_id, position, name)
VALUES (?, ?, ?);

-- name: InsertImportedTerm :exec
INSERT INTO terms (id, facet, name, display_name, slug, parent_id, related_affiliation_id, description_md, position,
    current_revision_id, legacy_wiki_pageid, legacy_title)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertTermAlias :exec
INSERT INTO term_aliases (term_id, name)
VALUES (?, ?);

-- name: InsertTermAncestor :exec
INSERT INTO term_ancestors (ancestor_id, descendant_id, depth)
VALUES (?, ?, ?);

-- name: InsertImportedCollection :exec
INSERT INTO collections (id, name, sort_name, sort_letter, url, kind, description_md, region_term_id, parent_id,
    is_bardic_resource, enumerable, current_revision_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertImportedEvent :exec
INSERT INTO events (id, slug, name, kind, description_md, current_revision_id, legacy_wiki_pageid, legacy_title)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertImportedPerson :exec
INSERT INTO persons (id, slug, name, sort_name, sort_letter, kind, status, status_date, status_source_url, pronouns,
    notes_md, licensing_notes_md, licensing_status, is_stub, current_revision_id, legacy_wiki_pageid, legacy_title)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertPersonAlias :exec
INSERT INTO person_aliases (person_id, name, kind, is_public)
VALUES (?, ?, ?, ?);

-- name: InsertPersonRole :exec
INSERT INTO person_roles (person_id, role)
VALUES (?, ?);

-- name: InsertPersonAffiliation :exec
INSERT INTO person_affiliations (person_id, term_id)
VALUES (?, ?);

-- name: InsertPersonRelation :exec
INSERT INTO person_relations (from_person_id, to_person_id, type)
VALUES (?, ?, ?);

-- name: InsertPersonPreference :exec
INSERT INTO person_preferences (person_id, dimension, value_json, source_url)
VALUES (?, ?, ?, ?);

-- name: InsertImportedPiece :exec
INSERT INTO pieces (id, slug, title, sort_title, sort_letter, kind, conservation_status, is_stub, notes_md,
    version_notes_md, licensing_notes_md, licensing_status, licence, historical_source_md, period_start, period_end,
    legacy_had_inline_lyrics, current_revision_id, legacy_wiki_pageid, legacy_title)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertPieceSecondaryKind :exec
INSERT INTO piece_secondary_kinds (piece_id, kind)
VALUES (?, ?);

-- name: InsertPieceAlias :exec
INSERT INTO piece_aliases (piece_id, text, kind)
VALUES (?, ?, ?);

-- name: InsertPieceTerm :exec
INSERT INTO piece_terms (piece_id, term_id, is_primary, part, position)
VALUES (?, ?, ?, ?, ?);

-- name: InsertPieceRelation :exec
INSERT INTO piece_relations (from_piece_id, to_piece_id, type)
VALUES (?, ?, ?);

-- name: InsertPieceCredit :exec
INSERT INTO piece_credits (piece_id, person_id, role, attribution, certainty, part, note, position)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertPiecePreference :exec
INSERT INTO piece_preferences (piece_id, dimension, value_json, source_url)
VALUES (?, ?, ?, ?);

-- name: InsertConservationHistory :exec
INSERT INTO conservation_history (piece_id, status, reason, source_url, revision_id, at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertImportedLink :exec
INSERT INTO links (id, url, normalized_url, title, description, platform, place_type, collection_id, locator,
    is_hidden)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertLinkAttachment :exec
INSERT INTO link_attachments (link_id, piece_id, person_id, role, position)
VALUES (?, ?, ?, ?, ?);

-- name: InsertPerformance :execlastid
INSERT INTO performances (link_id, piece_id, event_id, year, uploader, description_verbatim)
VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertPerformancePerformer :exec
INSERT INTO performance_performers (performance_id, person_id, position)
VALUES (?, ?, ?);
