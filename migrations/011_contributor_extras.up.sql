-- Contributor extras. Every statement is additive and safe while the previous release serves: it ignores the new
-- columns and the new table.

-- public_id is the opaque handle of a user in public URLs, set at creation and by a startup backfill, so it is NULL
-- only for a row created by a release that does not know the column.
ALTER TABLE `users`
    ADD COLUMN `public_id` CHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ADD UNIQUE INDEX `idx_public_id` (`public_id`);

CREATE TABLE `repertoire` (
    `user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `notes` VARCHAR(2000) NULL,
    -- Private by default, so an entry whose visibility is forgotten is never listed publicly.
    `visibility` ENUM('private', 'public') NOT NULL DEFAULT 'private',
    `added_at` DATETIME NOT NULL,
    `updated_at` DATETIME NOT NULL,
    PRIMARY KEY (`user_id`, `piece_id`),
    INDEX `idx_user_added` (`user_id`, `added_at`, `piece_id`),
    INDEX `idx_user_visibility_added` (`user_id`, `visibility`, `added_at`, `piece_id`),
    INDEX `idx_piece` (`piece_id`),
    CONSTRAINT `fk_repertoire_user_id` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_repertoire_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- The link checker's state: consecutive definite failures, and when the Wayback Machine was last asked.
ALTER TABLE `links`
    ADD COLUMN `liveness_failures` TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER `liveness`,
    ADD COLUMN `archive_checked_at` DATETIME NULL AFTER `archived_url`,
    ADD INDEX `idx_check_due` (`is_hidden`, `last_checked_at`, `id`),
    ADD INDEX `idx_archive_due` (`is_hidden`, `archive_checked_at`, `id`);

ALTER TABLE `link_reports`
    ADD INDEX `idx_reporter_link_status` (`reporter_user_id`, `link_id`, `status`, `resolved_at`);
