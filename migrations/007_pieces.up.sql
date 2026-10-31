CREATE TABLE `pieces` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `slug` VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `title` VARCHAR(255) NOT NULL,
    `sort_title` VARCHAR(255) NOT NULL,
    `sort_letter` CHAR(1) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `kind` ENUM('song', 'poem', 'story', 'dance_music', 'instrumental', 'comedy') NOT NULL,
    `conservation_status` ENUM('unassessed', 'low_risk', 'endangered', 'critically_endangered', 'restricted')
        NOT NULL DEFAULT 'unassessed',
    `is_stub` BOOL NOT NULL DEFAULT FALSE,
    `notes_md` TEXT NULL,
    `version_notes_md` TEXT NULL,
    `licensing_notes_md` TEXT NULL,
    `licensing_status` ENUM('known', 'unknown') NOT NULL DEFAULT 'unknown',
    `licence` ENUM('pd', 'cc_by', 'cc_by_sa', 'cc_by_nc', 'cc_by_nc_sa', 'cc_by_nd', 'cc_by_nc_nd', 'arr', 'other')
        NULL,
    `historical_source_md` TEXT NULL,
    `period_start` SMALLINT NULL,
    `period_end` SMALLINT NULL,
    `legacy_had_inline_lyrics` BOOL NOT NULL DEFAULT FALSE,
    `current_revision_id` BIGINT UNSIGNED NULL,
    `legacy_wiki_pageid` INT UNSIGNED NULL,
    `legacy_title` VARCHAR(255) NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_slug` (`slug`),
    UNIQUE INDEX `idx_legacy_wiki_pageid` (`legacy_wiki_pageid`),
    FULLTEXT INDEX `idx_ft_title_notes` (`title`, `notes_md`),
    INDEX `idx_sort_letter_title` (`sort_letter`, `sort_title`, `id`),
    INDEX `idx_sort_title` (`sort_title`, `id`),
    INDEX `idx_conservation_status` (`conservation_status`),
    INDEX `idx_kind` (`kind`),
    CONSTRAINT `fk_pieces_current_revision_id` FOREIGN KEY (`current_revision_id`) REFERENCES `revisions` (`id`),
    CONSTRAINT `chk_pieces_period` CHECK (`period_start` IS NULL OR `period_end` IS NULL OR `period_start` <= `period_end`),
    CONSTRAINT `chk_pieces_licence_known` CHECK (`licence` IS NULL OR `licensing_status` = 'known')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `piece_secondary_kinds` (
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `kind` ENUM('song', 'poem', 'story', 'dance_music', 'instrumental', 'comedy') NOT NULL,
    PRIMARY KEY (`piece_id`, `kind`),
    INDEX `idx_kind` (`kind`),
    CONSTRAINT `fk_piece_secondary_kinds_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `piece_aliases` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `text` VARCHAR(255) NOT NULL,
    `kind` ENUM('alt_title', 'nickname', 'subtitle', 'redirect') NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_piece_text` (`piece_id`, `text`),
    FULLTEXT INDEX `idx_ft_text` (`text`),
    CONSTRAINT `fk_piece_aliases_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- One origin term cannot carry two parts; at most one primary term per (piece, facet) is validated by the
-- revision engine.
CREATE TABLE `piece_terms` (
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `term_id` BIGINT UNSIGNED NOT NULL,
    `is_primary` BOOL NOT NULL DEFAULT FALSE,
    `part` ENUM('words', 'tune', 'verses', 'chorus') NULL,
    `position` SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (`piece_id`, `term_id`),
    INDEX `idx_term_piece` (`term_id`, `piece_id`),
    CONSTRAINT `fk_piece_terms_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_piece_terms_term_id` FOREIGN KEY (`term_id`) REFERENCES `terms` (`id`) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `piece_relations` (
    `from_piece_id` BIGINT UNSIGNED NOT NULL,
    `to_piece_id` BIGINT UNSIGNED NOT NULL,
    `type` ENUM('variant_of', 'contrafactum_of', 'setting_of', 'same_text_other_form') NOT NULL,
    PRIMARY KEY (`from_piece_id`, `to_piece_id`, `type`),
    INDEX `idx_to_piece` (`to_piece_id`),
    CONSTRAINT `fk_piece_relations_from_piece_id` FOREIGN KEY (`from_piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_piece_relations_to_piece_id` FOREIGN KEY (`to_piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `chk_piece_relations_not_self` CHECK (`from_piece_id` <> `to_piece_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `piece_credits` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `person_id` BIGINT UNSIGNED NULL,
    `role` ENUM('lyrics', 'tune', 'author', 'choreography', 'music', 'arrangement', 'translation') NOT NULL,
    `attribution` ENUM('named', 'traditional', 'unknown', 'anonymous', 'various') NOT NULL DEFAULT 'named',
    `certainty` ENUM('certain', 'probable', 'possible', 'disputed') NOT NULL DEFAULT 'certain',
    `part` ENUM('words', 'tune', 'verses', 'chorus') NULL,
    `note` VARCHAR(500) NULL,
    `position` SMALLINT UNSIGNED NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_piece_position` (`piece_id`, `position`),
    INDEX `idx_person` (`person_id`),
    CONSTRAINT `fk_piece_credits_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_piece_credits_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`) ON DELETE RESTRICT,
    CONSTRAINT `chk_piece_credits_named_person` CHECK ((`attribution` = 'named') = (`person_id` IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `conservation_history` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `status` ENUM('unassessed', 'low_risk', 'endangered', 'critically_endangered', 'restricted') NOT NULL,
    `reason` TEXT NOT NULL,
    `source_url` VARCHAR(2048) NULL,
    `revision_id` BIGINT UNSIGNED NULL,
    `at` DATETIME NOT NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_piece_at` (`piece_id`, `at`, `id`),
    CONSTRAINT `fk_conservation_history_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_conservation_history_revision_id` FOREIGN KEY (`revision_id`) REFERENCES `revisions` (`id`)
        ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `piece_preferences` (
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `dimension` ENUM('licensing', 'performance_permission', 'adaptations', 'attribution', 'commercial_use',
        'recording', 'performance_etiquette', 'objectionable_use') NOT NULL,
    `value_json` JSON NOT NULL,
    `source_url` VARCHAR(2048) NOT NULL,
    PRIMARY KEY (`piece_id`, `dimension`),
    CONSTRAINT `fk_piece_preferences_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
