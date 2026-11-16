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
-- Catalogue: public reads
--
-- An entity is public when it has an approved revision (current_revision_id IS NOT NULL); a term also needs
-- every ancestor approved (the public_terms view). Persons who prefer no page are left out of the lists of persons
-- (the listed_persons view) but named, without a link, wherever else they are referenced. Hidden links are left out
-- of every read. What a link records is shown on
-- another entity's page only if the link has a public attachment (the public_link_attachments view).
-- ============================================

-- name: GetPublicPiece :one
SELECT p.id, p.slug, p.title, p.kind, p.conservation_status, p.is_stub, p.notes_md, p.version_notes_md,
    p.licensing_status, p.licensing_notes_md, COALESCE(r.reviewed_at, r.created_at) AS updated_at
FROM pieces p
JOIN revisions r ON r.id = p.current_revision_id
WHERE p.slug = ?;

-- name: ListPieceCreditNames :many
-- Named credits of public pieces to public persons, in credit order.
SELECT pc.slug AS piece_slug, p.slug AS person_slug, p.name AS person_name
FROM piece_credits c
JOIN pieces pc ON pc.id = c.piece_id
JOIN persons p ON p.id = c.person_id
WHERE pc.slug IN (sqlc.slice('slugs')) AND p.current_revision_id IS NOT NULL
ORDER BY pc.slug, c.position, c.id;

-- name: ListPieceAliases :many
SELECT text, kind
FROM piece_aliases
WHERE piece_id = ?
ORDER BY id;

-- name: ListPublicPieceTerms :many
SELECT t.slug, t.facet, t.display_name
FROM piece_terms pt
JOIN public_terms t ON t.id = pt.term_id
WHERE pt.piece_id = ?
ORDER BY t.facet, pt.position, t.id;

-- name: ListPublicPieceCredits :many
SELECT c.role, c.attribution, c.certainty,
    p.slug AS person_slug, p.name AS person_name, p.kind AS person_kind, p.is_stub AS person_is_stub,
    p.status AS person_status
FROM piece_credits c
LEFT JOIN persons p ON p.id = c.person_id AND p.current_revision_id IS NOT NULL
WHERE c.piece_id = ? AND (c.person_id IS NULL OR p.id IS NOT NULL)
ORDER BY c.position, c.id;

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
    e.slug AS event_slug, e.name AS event_name
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN events e ON e.id = perf.event_id AND e.current_revision_id IS NOT NULL
WHERE perf.piece_id = ? AND NOT l.is_hidden
    AND EXISTS (SELECT 1 FROM link_attachments a WHERE a.link_id = perf.link_id AND a.piece_id = perf.piece_id)
ORDER BY perf.link_id, perf.id;

-- name: ListPublicPerformers :many
SELECT pp.performance_id, p.slug, p.name, p.kind, p.is_stub, p.status
FROM performance_performers pp
JOIN persons p ON p.id = pp.person_id
WHERE pp.performance_id IN (sqlc.slice('performance_ids')) AND p.current_revision_id IS NOT NULL
ORDER BY pp.performance_id, pp.position;

-- name: GetPublicPerson :one
SELECT p.id, p.slug, p.name, p.kind, p.status, p.is_stub, p.notes_md, p.licensing_status, p.licensing_notes_md,
    COALESCE(r.reviewed_at, r.created_at) AS updated_at
FROM persons p
JOIN revisions r ON r.id = p.current_revision_id
WHERE p.slug = ?;

-- name: ListPersonAliases :many
SELECT name, kind
FROM person_aliases
WHERE person_id = ?
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
SELECT c.role, c.certainty, p.slug, p.title, p.kind, p.conservation_status, p.is_stub
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
-- a public attachment, as a performance only an unapproved entity's links record may name them. A
-- performance of a piece that is not public is left out entirely, as its description may name the piece.

-- name: ListPublicPersonPerformances :many
SELECT perf.year, perf.description_verbatim, l.url, l.title, l.platform,
    p.slug AS piece_slug, p.title AS piece_title, p.kind AS piece_kind,
    p.conservation_status AS piece_conservation_status, p.is_stub AS piece_is_stub,
    e.slug AS event_slug, e.name AS event_name
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
    ra.slug AS related_slug, ra.facet AS related_facet, ra.display_name AS related_display_name,
    COALESCE(r.reviewed_at, r.created_at) AS updated_at
FROM public_terms t
JOIN revisions r ON r.id = t.current_revision_id
LEFT JOIN public_terms ra ON ra.id = t.related_affiliation_id
WHERE t.slug = ?;

-- A term's counts are of the distinct public pieces that are not stubs tagged with, and public persons who are not
-- stubs affiliated with, the term or any public descendant: both ends of the closure are checked, so a pending term
-- hides its subtree from its ancestors' counts too. A term with no row of a kind has a count of 0 of it. Its member
-- lists below leave out the same stubs, so the counts are their totals.

-- name: ListPublicTermCounts :many
SELECT ta.ancestor_id AS term_id, 'piece' AS member, COUNT(DISTINCT pt.piece_id) AS n
FROM term_ancestors ta
JOIN public_terms d ON d.id = ta.descendant_id
JOIN piece_terms pt ON pt.term_id = ta.descendant_id
JOIN pieces p ON p.id = pt.piece_id
WHERE ta.ancestor_id IN (sqlc.slice('piece_term_ids')) AND p.current_revision_id IS NOT NULL AND NOT p.is_stub
GROUP BY ta.ancestor_id
UNION ALL
SELECT ta.ancestor_id, 'person', COUNT(DISTINCT pa.person_id)
FROM term_ancestors ta
JOIN public_terms d ON d.id = ta.descendant_id
JOIN person_affiliations pa ON pa.term_id = ta.descendant_id
JOIN listed_persons p ON p.id = pa.person_id
WHERE ta.ancestor_id IN (sqlc.slice('person_term_ids')) AND NOT p.is_stub
GROUP BY ta.ancestor_id;

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
WHERE p.current_revision_id IS NOT NULL AND NOT p.is_stub
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
WHERE NOT p.is_stub
    AND EXISTS (
        SELECT 1
        FROM person_affiliations pa
        JOIN term_ancestors ta ON ta.descendant_id = pa.term_id
        JOIN public_terms d ON d.id = ta.descendant_id
        WHERE pa.person_id = p.id AND ta.ancestor_id = sqlc.arg(term_id)
    )
ORDER BY p.sort_name, p.id
LIMIT ? OFFSET ?;

-- name: GetPublicCollection :one
SELECT c.id, c.name, c.kind, c.url, c.description_md,
    r.slug AS region_slug, r.facet AS region_facet, r.display_name AS region_display_name,
    COALESCE(rev.reviewed_at, rev.created_at) AS updated_at
FROM collections c
JOIN revisions rev ON rev.id = c.current_revision_id
LEFT JOIN public_terms r ON r.id = c.region_term_id
WHERE c.id = ? AND c.current_revision_id IS NOT NULL;

-- A collection's citations are its visible links, once per public piece or public person that each
-- is attached to, whatever roles it is attached with.

-- name: ListPublicCollectionCitations :many
SELECT DISTINCT l.id AS link_id, l.url, l.title, l.locator, a.piece_id, a.person_id,
    COALESCE(p.sort_title, pe.sort_name) AS sort_key,
    p.slug AS piece_slug, p.title AS piece_title, p.kind AS piece_kind,
    p.conservation_status AS piece_conservation_status, p.is_stub AS piece_is_stub,
    pe.slug AS person_slug, pe.name AS person_name, pe.kind AS person_kind, pe.is_stub AS person_is_stub,
    pe.status AS person_status
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
SELECT slug, name
FROM events
WHERE current_revision_id IS NOT NULL
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountPublicEvents :one
SELECT COUNT(*)
FROM events
WHERE current_revision_id IS NOT NULL;

-- name: GetPublicEvent :one
SELECT e.id, e.slug, e.name, COALESCE(r.reviewed_at, r.created_at) AS updated_at
FROM events e
JOIN revisions r ON r.id = e.current_revision_id
WHERE e.slug = ?;

-- An event's performances are on visible links attached to a public piece or a public person, as
-- a performance only an unapproved entity's links record may name them. A performance of a piece
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

-- name: GetRevision :one
SELECT r.id, r.entity_type, r.entity_id, r.base_revision_id, r.source, r.status, r.summary,
    r.created_at, r.snapshot_json, u.display_name AS author_display_name
FROM revisions r
LEFT JOIN users u ON u.id = r.author_user_id
WHERE r.id = ?;

-- name: GetCurrentRevisionID :one
SELECT x.current_revision_id FROM pieces x WHERE sqlc.arg(entity_type) = 'piece' AND x.id = sqlc.arg(id)
UNION ALL SELECT x.current_revision_id FROM persons x WHERE sqlc.arg(entity_type) = 'person' AND x.id = sqlc.arg(id)
UNION ALL SELECT x.current_revision_id FROM terms x WHERE sqlc.arg(entity_type) = 'term' AND x.id = sqlc.arg(id)
UNION ALL SELECT x.current_revision_id FROM collections x WHERE sqlc.arg(entity_type) = 'collection' AND x.id = sqlc.arg(id)
UNION ALL SELECT x.current_revision_id FROM events x WHERE sqlc.arg(entity_type) = 'event' AND x.id = sqlc.arg(id);

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

-- name: ListPublicTermsByID :many
SELECT t.id, t.slug, t.facet, t.display_name
FROM public_terms t
WHERE t.id IN (sqlc.slice('ids'));

-- name: ListPublicCollectionsByID :many
SELECT id, name, kind, url
FROM collections
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- name: ListPublicEventsByID :many
SELECT id, slug, name
FROM events
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- The hidden links among a snapshot's, found by normalized URL as approval upserts them. Every link's normalized_url
-- is domain.NormalizeURL(url) (DEPLOY.md, "The normalized URL invariant"); the caller queries with normalized
-- (ASCII) URLs only.

-- name: ListHiddenNormalizedURLs :many
SELECT normalized_url
FROM links
WHERE is_hidden AND normalized_url IN (sqlc.slice('normalized_urls'));
