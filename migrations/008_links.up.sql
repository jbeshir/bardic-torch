CREATE TABLE `links` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `url` VARCHAR(2048) NOT NULL,
    `normalized_url` VARCHAR(2048) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `title` VARCHAR(500) NULL,
    `description` TEXT NULL,
    `platform` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    `place_type` ENUM('recording_platform', 'streaming', 'author_site', 'community_archive', 'physical_archive',
        'book', 'vendor', 'author_contact', 'oral_tradition', 'other') NULL,
    `collection_id` BIGINT UNSIGNED NULL,
    `locator` VARCHAR(255) NULL,
    `liveness` ENUM('unknown', 'ok', 'dead', 'blocked') NOT NULL DEFAULT 'unknown',
    `last_checked_at` DATETIME NULL,
    `archived_url` VARCHAR(2048) NULL,
    `is_hidden` BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_normalized_url` (`normalized_url`),
    INDEX `idx_collection` (`collection_id`),
    INDEX `idx_liveness` (`liveness`),
    CONSTRAINT `fk_links_collection_id` FOREIGN KEY (`collection_id`) REFERENCES `collections` (`id`)
        ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `link_attachments` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `link_id` BIGINT UNSIGNED NOT NULL,
    `piece_id` BIGINT UNSIGNED NULL,
    `person_id` BIGINT UNSIGNED NULL,
    `role` ENUM('official', 'lyrics', 'text', 'sheet_music', 'dance_instructions', 'text_source', 'translation',
        'performance', 'licensing_source', 'website', 'external_profile', 'other') NOT NULL,
    `position` SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_link_role_piece` (`link_id`, `role`, `piece_id`),
    UNIQUE INDEX `idx_link_role_person` (`link_id`, `role`, `person_id`),
    INDEX `idx_piece_position` (`piece_id`, `position`),
    INDEX `idx_person_position` (`person_id`, `position`),
    CONSTRAINT `fk_link_attachments_link_id` FOREIGN KEY (`link_id`) REFERENCES `links` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_link_attachments_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_link_attachments_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `chk_link_attachments_one_entity` CHECK ((`piece_id` IS NULL) <> (`person_id` IS NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- event_id is ON DELETE SET NULL, so it must never carry a CHECK (MySQL error 3823).
CREATE TABLE `performances` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `link_id` BIGINT UNSIGNED NOT NULL,
    `piece_id` BIGINT UNSIGNED NULL,
    `event_id` BIGINT UNSIGNED NULL,
    `year` SMALLINT NULL,
    `uploader` VARCHAR(255) NULL,
    `description_verbatim` TEXT NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_link` (`link_id`),
    INDEX `idx_piece` (`piece_id`),
    INDEX `idx_event_year` (`event_id`, `year`),
    CONSTRAINT `fk_performances_link_id` FOREIGN KEY (`link_id`) REFERENCES `links` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_performances_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_performances_event_id` FOREIGN KEY (`event_id`) REFERENCES `events` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `performance_performers` (
    `performance_id` BIGINT UNSIGNED NOT NULL,
    `person_id` BIGINT UNSIGNED NOT NULL,
    `position` SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (`performance_id`, `person_id`),
    INDEX `idx_person` (`person_id`),
    CONSTRAINT `fk_performance_performers_performance_id` FOREIGN KEY (`performance_id`)
        REFERENCES `performances` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_performance_performers_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`)
        ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
