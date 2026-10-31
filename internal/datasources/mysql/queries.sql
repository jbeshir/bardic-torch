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
ON DUPLICATE KEY UPDATE role = 'moderator';

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
