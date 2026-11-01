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
-- every ancestor approved, which the term_ancestors closure checks (it includes the term's own depth-0 row).
-- prefers_no_page persons are dropped from every list that references them, and hidden links from every read.
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
JOIN terms t ON t.id = pt.term_id
WHERE pt.piece_id = ?
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    )
ORDER BY t.facet, pt.position, t.id;

-- name: ListPublicPieceCredits :many
SELECT c.role, c.attribution, c.certainty, c.part, c.note,
    p.slug AS person_slug, p.name AS person_name, p.kind AS person_kind, p.is_stub AS person_is_stub
FROM piece_credits c
LEFT JOIN persons p ON p.id = c.person_id
WHERE c.piece_id = ?
    AND (c.person_id IS NULL OR (p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'))
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
JOIN persons p ON p.id = pp.person_id
WHERE pp.performance_id IN (sqlc.slice('performance_ids'))
    AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
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
JOIN terms t ON t.id = pa.term_id
WHERE pa.person_id = ?
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    )
ORDER BY t.position, t.display_name, t.id;

-- name: ListPublicPersonRelationsTo :many
SELECT r.type, p.slug, p.name, p.kind, p.is_stub
FROM person_relations r
JOIN persons p ON p.id = r.to_person_id
WHERE r.from_person_id = ? AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
ORDER BY r.type, p.sort_name, p.id;

-- name: ListPublicPersonRelationsFrom :many
SELECT r.type, p.slug, p.name, p.kind, p.is_stub
FROM person_relations r
JOIN persons p ON p.id = r.from_person_id
WHERE r.to_person_id = ? AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
ORDER BY r.type, p.sort_name, p.id;

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

-- A person's performances are those they perform in and those recorded by links attached to them. A
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
        EXISTS (SELECT 1 FROM performance_performers pp
            WHERE pp.performance_id = perf.id AND pp.person_id = sqlc.arg(performer_id))
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
        EXISTS (SELECT 1 FROM performance_performers pp
            WHERE pp.performance_id = perf.id AND pp.person_id = sqlc.arg(performer_id))
        OR EXISTS (SELECT 1 FROM link_attachments a
            WHERE a.link_id = perf.link_id AND a.person_id = sqlc.arg(attached_person_id))
    );

-- A term's counts are of the distinct public pieces tagged with, and public persons with a page affiliated
-- with, the term or any public descendant: both ends of the closure are checked, so a pending term hides its
-- subtree from its ancestors' counts too.

-- name: ListPublicTerms :many
SELECT t.slug, t.facet, t.name, t.display_name, parent.slug AS parent_slug,
    (SELECT COUNT(DISTINCT pt.piece_id)
        FROM term_ancestors ta
        JOIN piece_terms pt ON pt.term_id = ta.descendant_id
        JOIN pieces p ON p.id = pt.piece_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS piece_count,
    (SELECT COUNT(DISTINCT pa.person_id)
        FROM term_ancestors ta
        JOIN person_affiliations pa ON pa.term_id = ta.descendant_id
        JOIN persons p ON p.id = pa.person_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS person_count
FROM terms t
LEFT JOIN terms parent ON parent.id = t.parent_id
WHERE (sqlc.narg(facet) IS NULL OR t.facet = sqlc.narg(facet))
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    )
ORDER BY t.facet, t.position, t.display_name, t.id;

-- name: GetPublicTerm :one
SELECT t.id, t.slug, t.facet, t.name, t.display_name, t.description_md,
    ra.slug AS related_slug, ra.facet AS related_facet, ra.display_name AS related_display_name,
    (SELECT COUNT(DISTINCT pt.piece_id)
        FROM term_ancestors ta
        JOIN piece_terms pt ON pt.term_id = ta.descendant_id
        JOIN pieces p ON p.id = pt.piece_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS piece_count,
    (SELECT COUNT(DISTINCT pa.person_id)
        FROM term_ancestors ta
        JOIN person_affiliations pa ON pa.term_id = ta.descendant_id
        JOIN persons p ON p.id = pa.person_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS person_count
FROM terms t
LEFT JOIN terms ra ON ra.id = t.related_affiliation_id
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = ra.id AND anc.current_revision_id IS NULL
    )
WHERE t.slug = ?
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    );

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
SELECT t.slug, t.facet, t.display_name,
    (SELECT COUNT(DISTINCT pt.piece_id)
        FROM term_ancestors ta
        JOIN piece_terms pt ON pt.term_id = ta.descendant_id
        JOIN pieces p ON p.id = pt.piece_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS piece_count,
    (SELECT COUNT(DISTINCT pa.person_id)
        FROM term_ancestors ta
        JOIN person_affiliations pa ON pa.term_id = ta.descendant_id
        JOIN persons p ON p.id = pa.person_id
        WHERE ta.ancestor_id = t.id AND p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    ) AS person_count
FROM terms t
WHERE t.parent_id = ?
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    )
ORDER BY t.position, t.display_name, t.id;

-- name: ListPublicTermPieces :many
SELECT p.slug, p.title, p.kind, p.conservation_status, p.is_stub
FROM pieces p
WHERE p.current_revision_id IS NOT NULL
    AND EXISTS (
        SELECT 1
        FROM piece_terms pt
        JOIN term_ancestors ta ON ta.descendant_id = pt.term_id
        WHERE pt.piece_id = p.id AND ta.ancestor_id = sqlc.arg(term_id)
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    )
ORDER BY p.sort_title, p.id
LIMIT ? OFFSET ?;

-- name: ListPublicTermPersons :many
SELECT p.slug, p.name, p.kind, p.is_stub
FROM persons p
WHERE p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page'
    AND EXISTS (
        SELECT 1
        FROM person_affiliations pa
        JOIN term_ancestors ta ON ta.descendant_id = pa.term_id
        WHERE pa.person_id = p.id AND ta.ancestor_id = sqlc.arg(term_id)
            AND NOT EXISTS (
                SELECT 1 FROM term_ancestors da JOIN terms anc ON anc.id = da.ancestor_id
                WHERE da.descendant_id = ta.descendant_id AND anc.current_revision_id IS NULL
            )
    )
ORDER BY p.sort_name, p.id
LIMIT ? OFFSET ?;

-- name: GetPublicCollection :one
SELECT c.id, c.name, c.kind, c.url, c.is_bardic_resource, c.description_md, c.enumerable,
    r.slug AS region_slug, r.facet AS region_facet, r.display_name AS region_display_name,
    parent.id AS parent_id, parent.name AS parent_name, parent.kind AS parent_kind, parent.url AS parent_url,
    parent.is_bardic_resource AS parent_is_bardic_resource
FROM collections c
LEFT JOIN terms r ON r.id = c.region_term_id
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = r.id AND anc.current_revision_id IS NULL
    )
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
JOIN link_attachments a ON a.link_id = l.id
LEFT JOIN pieces p ON p.id = a.piece_id AND p.current_revision_id IS NOT NULL
LEFT JOIN persons pe ON pe.id = a.person_id
    AND pe.current_revision_id IS NOT NULL AND pe.status <> 'prefers_no_page'
WHERE l.collection_id = ? AND NOT l.is_hidden AND (p.id IS NOT NULL OR pe.id IS NOT NULL)
ORDER BY sort_key, l.id, a.piece_id, a.person_id
LIMIT ?;

-- name: CountPublicCollectionCitations :one
SELECT COUNT(*)
FROM (
    SELECT DISTINCT l.id, a.piece_id, a.person_id
    FROM links l
    JOIN link_attachments a ON a.link_id = l.id
    LEFT JOIN pieces p ON p.id = a.piece_id AND p.current_revision_id IS NOT NULL
    LEFT JOIN persons pe ON pe.id = a.person_id
        AND pe.current_revision_id IS NOT NULL AND pe.status <> 'prefers_no_page'
    WHERE l.collection_id = ? AND NOT l.is_hidden AND (p.id IS NOT NULL OR pe.id IS NOT NULL)
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
    AND EXISTS (
        SELECT 1
        FROM link_attachments a
        LEFT JOIN pieces ap ON ap.id = a.piece_id
        LEFT JOIN persons apr ON apr.id = a.person_id
        WHERE a.link_id = perf.link_id
            AND (ap.current_revision_id IS NOT NULL
                OR (apr.current_revision_id IS NOT NULL AND apr.status <> 'prefers_no_page'))
    )
ORDER BY perf.year IS NULL, perf.year DESC, perf.id
LIMIT ?;

-- name: CountPublicEventPerformances :one
SELECT COUNT(*)
FROM performances perf
JOIN links l ON l.id = perf.link_id
LEFT JOIN pieces p ON p.id = perf.piece_id
WHERE perf.event_id = ? AND NOT l.is_hidden
    AND (perf.piece_id IS NULL OR p.current_revision_id IS NOT NULL)
    AND EXISTS (
        SELECT 1
        FROM link_attachments a
        LEFT JOIN pieces ap ON ap.id = a.piece_id
        LEFT JOIN persons apr ON apr.id = a.person_id
        WHERE a.link_id = perf.link_id
            AND (ap.current_revision_id IS NOT NULL
                OR (apr.current_revision_id IS NOT NULL AND apr.status <> 'prefers_no_page'))
    );

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
FROM terms t
WHERE t.slug = ?
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    );

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
FROM terms t
WHERE t.id IN (sqlc.slice('ids'))
    AND NOT EXISTS (
        SELECT 1 FROM term_ancestors ta JOIN terms anc ON anc.id = ta.ancestor_id
        WHERE ta.descendant_id = t.id AND anc.current_revision_id IS NULL
    );

-- name: ListPublicCollectionsByID :many
SELECT id, name, kind, url, is_bardic_resource
FROM collections
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- name: ListPublicEventsByID :many
SELECT id, slug, name, kind
FROM events
WHERE id IN (sqlc.slice('ids')) AND current_revision_id IS NOT NULL;

-- A snapshot's link is hidden if a hidden link has its URL as entered or normalized.

-- name: ListHiddenLinkURLs :many
SELECT url, normalized_url
FROM links
WHERE is_hidden AND (url IN (sqlc.slice('urls')) OR normalized_url IN (sqlc.slice('normalized_urls')));
