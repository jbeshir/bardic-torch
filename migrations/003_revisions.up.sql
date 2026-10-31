CREATE TABLE `revisions` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `entity_type` ENUM('piece', 'person', 'term', 'collection', 'event') NOT NULL,
    `entity_id` BIGINT UNSIGNED NOT NULL,
    `base_revision_id` BIGINT UNSIGNED NULL,
    `author_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    `source` ENUM('user', 'import', 'mcp') NOT NULL,
    `status` ENUM('draft', 'pending', 'approved', 'rejected', 'withdrawn') NOT NULL,
    `snapshot_json` JSON NOT NULL,
    `summary` VARCHAR(500) NOT NULL DEFAULT '',
    `reviewer_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    `reviewed_at` DATETIME NULL,
    `review_note` TEXT NULL,
    `created_at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_entity_status_created` (`entity_type`, `entity_id`, `status`, `created_at`, `id`),
    INDEX `idx_author_status` (`author_user_id`, `status`),
    INDEX `idx_status_created` (`status`, `created_at`),
    CONSTRAINT `fk_revisions_base_revision_id` FOREIGN KEY (`base_revision_id`) REFERENCES `revisions` (`id`),
    CONSTRAINT `fk_revisions_author_user_id` FOREIGN KEY (`author_user_id`) REFERENCES `users` (`id`),
    CONSTRAINT `fk_revisions_reviewer_user_id` FOREIGN KEY (`reviewer_user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `revision_contributors` (
    `revision_id` BIGINT UNSIGNED NOT NULL,
    `position` SMALLINT UNSIGNED NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    PRIMARY KEY (`revision_id`, `position`),
    CONSTRAINT `fk_revision_contributors_revision_id` FOREIGN KEY (`revision_id`) REFERENCES `revisions` (`id`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
