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
