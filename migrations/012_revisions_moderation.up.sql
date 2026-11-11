-- BE4: revisions and moderation. Every statement is additive, so the previous release keeps working against it.

-- A new entity's revision has no entity row until it is approved.
ALTER TABLE `revisions`
    MODIFY `entity_id` BIGINT UNSIGNED NULL;

-- Which document owns each performance: the piece's or the person's, so that approving one document rewrites only
-- the performances it records.
ALTER TABLE `performances`
    ADD COLUMN `owner_piece_id` BIGINT UNSIGNED NULL,
    ADD COLUMN `owner_person_id` BIGINT UNSIGNED NULL,
    ADD INDEX `idx_owner_piece` (`owner_piece_id`),
    ADD INDEX `idx_owner_person` (`owner_person_id`),
    ADD CONSTRAINT `fk_performances_owner_piece_id` FOREIGN KEY (`owner_piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE,
    ADD CONSTRAINT `fk_performances_owner_person_id` FOREIGN KEY (`owner_person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE;

-- The link checker's state: consecutive definite failures, and when the Wayback Machine was last asked.
ALTER TABLE `links`
    ADD COLUMN `liveness_failures` TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER `liveness`,
    ADD COLUMN `archive_checked_at` DATETIME NULL AFTER `archived_url`,
    ADD INDEX `idx_check_due` (`is_hidden`, `last_checked_at`, `id`),
    ADD INDEX `idx_archive_due` (`is_hidden`, `archive_checked_at`, `id`);

-- The version of a Creative Commons licence. Reads show it only beside a cc_* licence.
ALTER TABLE `pieces`
    ADD COLUMN `licence_version` ENUM('1.0', '2.0', '2.5', '3.0', '4.0') NULL AFTER `licence`;

CREATE TABLE `link_reports` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `link_id` BIGINT UNSIGNED NOT NULL,
    `reporter_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    `reason` ENUM('broken', 'wrong_link', 'privacy', 'copyright', 'lyrics', 'other') NOT NULL,
    `message` TEXT NULL,
    `status` ENUM('open', 'resolved', 'dismissed') NOT NULL DEFAULT 'open',
    `created_at` DATETIME NOT NULL,
    `resolved_by_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    `resolved_at` DATETIME NULL,
    `resolution_note` TEXT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_status_created` (`status`, `created_at`, `id`),
    INDEX `idx_reporter_link_status` (`reporter_user_id`, `link_id`, `status`),
    CONSTRAINT `fk_link_reports_link_id` FOREIGN KEY (`link_id`) REFERENCES `links` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_link_reports_reporter_user_id` FOREIGN KEY (`reporter_user_id`) REFERENCES `users` (`id`),
    CONSTRAINT `fk_link_reports_resolved_by_user_id` FOREIGN KEY (`resolved_by_user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `contact_requests` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `kind` ENUM('takedown', 'opt_out', 'correction', 'general') NOT NULL,
    -- name, email, message and url are personal data: nulled 180 days after resolution or dismissal.
    `name` VARCHAR(255) NULL,
    `email` VARCHAR(254) NULL,
    `message` TEXT NULL,
    `url` VARCHAR(2048) NULL,
    `status` ENUM('open', 'resolved', 'dismissed') NOT NULL DEFAULT 'open',
    `created_at` DATETIME NOT NULL,
    `resolved_by_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    `resolved_at` DATETIME NULL,
    `resolution_note` TEXT NULL,
    `purged_at` DATETIME NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_status_created` (`status`, `created_at`, `id`),
    INDEX `idx_resolved_purged` (`resolved_at`, `purged_at`),
    CONSTRAINT `fk_contact_requests_resolved_by_user_id` FOREIGN KEY (`resolved_by_user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
