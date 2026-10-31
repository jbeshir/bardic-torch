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
