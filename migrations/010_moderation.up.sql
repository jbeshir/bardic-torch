-- Drafts of new entities have no entity yet; every other revision has one.
ALTER TABLE `revisions`
    MODIFY `entity_id` BIGINT UNSIGNED NULL,
    ADD COLUMN `submitted_snapshot_json` JSON NULL AFTER `snapshot_json`,
    ADD COLUMN `flags_json` JSON NULL AFTER `submitted_snapshot_json`,
    ADD COLUMN `received_keys_json` JSON NULL AFTER `flags_json`,
    ADD COLUMN `submitted_base_revision_id` BIGINT UNSIGNED NULL AFTER `base_revision_id`,
    ADD COLUMN `updated_at` DATETIME NULL AFTER `created_at`,
    ADD COLUMN `submitted_at` DATETIME NULL AFTER `updated_at`,
    ADD COLUMN `rebased_by_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL
        AFTER `reviewer_user_id`,
    ADD COLUMN `rebased_at` DATETIME NULL AFTER `rebased_by_user_id`,
    ADD INDEX `idx_status_submitted` (`status`, `submitted_at`, `id`),
    ADD INDEX `idx_author_updated` (`author_user_id`, `status`, `updated_at`, `id`),
    ADD CONSTRAINT `fk_revisions_rebased_by_user_id` FOREIGN KEY (`rebased_by_user_id`) REFERENCES `users` (`id`),
    ADD CONSTRAINT `fk_revisions_submitted_base_revision_id` FOREIGN KEY (`submitted_base_revision_id`)
        REFERENCES `revisions` (`id`),
    ADD CONSTRAINT `chk_revisions_entity_unless_draft` CHECK (`entity_id` IS NOT NULL OR `status` = 'draft');

-- Which document owns each performance: the piece's or the person's.
ALTER TABLE `performances`
    ADD COLUMN `owner_piece_id` BIGINT UNSIGNED NULL,
    ADD COLUMN `owner_person_id` BIGINT UNSIGNED NULL,
    ADD INDEX `idx_owner_piece` (`owner_piece_id`),
    ADD INDEX `idx_owner_person` (`owner_person_id`),
    ADD CONSTRAINT `fk_performances_owner_piece_id` FOREIGN KEY (`owner_piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE,
    ADD CONSTRAINT `fk_performances_owner_person_id` FOREIGN KEY (`owner_person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE,
    ADD CONSTRAINT `chk_performances_one_owner` CHECK (`owner_piece_id` IS NULL OR `owner_person_id` IS NULL);

UPDATE `performances` perf
SET perf.`owner_piece_id` = perf.`piece_id`
WHERE perf.`piece_id` IS NOT NULL AND EXISTS (
    SELECT 1 FROM `link_attachments` a WHERE a.`link_id` = perf.`link_id` AND a.`piece_id` = perf.`piece_id`);

UPDATE `performances` perf
JOIN (SELECT `link_id`, MIN(`person_id`) AS `person_id`
      FROM `link_attachments` WHERE `person_id` IS NOT NULL GROUP BY `link_id`) owner
    ON owner.`link_id` = perf.`link_id`
SET perf.`owner_person_id` = owner.`person_id`
WHERE perf.`owner_piece_id` IS NULL;

-- Display-name impersonation control: the folded key, unique; NULL until written.
ALTER TABLE `users`
    ADD COLUMN `display_name_key` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    ADD UNIQUE INDEX `idx_display_name_key` (`display_name_key`);

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
    INDEX `idx_reporter_status` (`reporter_user_id`, `status`),
    INDEX `idx_link_status` (`link_id`, `status`),
    CONSTRAINT `fk_link_reports_link_id` FOREIGN KEY (`link_id`) REFERENCES `links` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_link_reports_reporter_user_id` FOREIGN KEY (`reporter_user_id`) REFERENCES `users` (`id`),
    CONSTRAINT `fk_link_reports_resolved_by_user_id` FOREIGN KEY (`resolved_by_user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `contact_requests` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `kind` ENUM('takedown', 'opt_out', 'correction', 'general') NOT NULL,
    -- name, email, message and url are personal data: nulled 180 days after resolution.
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
