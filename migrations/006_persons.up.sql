CREATE TABLE `persons` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `slug` VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `sort_name` VARCHAR(255) NOT NULL,
    `sort_letter` CHAR(1) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `kind` ENUM('community_bard', 'historical', 'group') NOT NULL,
    -- That a status needs a source is validated by the revision engine, not by a CHECK.
    `status` ENUM('active', 'deceased', 'prefers_no_page') NOT NULL DEFAULT 'active',
    `status_date` DATE NULL,
    `status_source_url` VARCHAR(2048) NULL,
    `pronouns` VARCHAR(64) NULL,
    `notes_md` TEXT NULL,
    `licensing_notes_md` TEXT NULL,
    `licensing_status` ENUM('known', 'unknown') NOT NULL DEFAULT 'unknown',
    `is_stub` BOOL NOT NULL DEFAULT FALSE,
    `claimed_by_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL,
    `current_revision_id` BIGINT UNSIGNED NULL,
    `legacy_wiki_pageid` INT UNSIGNED NULL,
    `legacy_title` VARCHAR(255) NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_slug` (`slug`),
    UNIQUE INDEX `idx_legacy_wiki_pageid` (`legacy_wiki_pageid`),
    FULLTEXT INDEX `idx_ft_name` (`name`),
    INDEX `idx_sort_letter_name` (`sort_letter`, `sort_name`, `id`),
    INDEX `idx_sort_name` (`sort_name`, `id`),
    CONSTRAINT `fk_persons_claimed_by_user_id` FOREIGN KEY (`claimed_by_user_id`) REFERENCES `users` (`id`),
    CONSTRAINT `fk_persons_current_revision_id` FOREIGN KEY (`current_revision_id`) REFERENCES `revisions` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `person_aliases` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `person_id` BIGINT UNSIGNED NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `kind` ENUM('legal', 'sca', 'stage', 'nickname', 'former', 'redirect') NOT NULL,
    -- Defaults to hidden, so that an alias inserted without a value fails closed.
    `is_public` BOOL NOT NULL DEFAULT FALSE,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_person_name` (`person_id`, `name`),
    FULLTEXT INDEX `idx_ft_name` (`name`),
    CONSTRAINT `fk_person_aliases_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `person_roles` (
    `person_id` BIGINT UNSIGNED NOT NULL,
    `role` ENUM('singer', 'songwriter', 'musician', 'instrumentalist', 'storyteller', 'poet', 'jester', 'dancer')
        NOT NULL,
    PRIMARY KEY (`person_id`, `role`),
    INDEX `idx_role` (`role`),
    CONSTRAINT `fk_person_roles_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `person_affiliations` (
    `person_id` BIGINT UNSIGNED NOT NULL,
    `term_id` BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (`person_id`, `term_id`),
    INDEX `idx_term` (`term_id`),
    CONSTRAINT `fk_person_affiliations_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_person_affiliations_term_id` FOREIGN KEY (`term_id`) REFERENCES `terms` (`id`) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `person_relations` (
    `from_person_id` BIGINT UNSIGNED NOT NULL,
    `to_person_id` BIGINT UNSIGNED NOT NULL,
    `type` ENUM('heir_of', 'member_of', 'same_as') NOT NULL,
    PRIMARY KEY (`from_person_id`, `to_person_id`, `type`),
    INDEX `idx_to_person` (`to_person_id`),
    CONSTRAINT `fk_person_relations_from_person_id` FOREIGN KEY (`from_person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_person_relations_to_person_id` FOREIGN KEY (`to_person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `chk_person_relations_not_self` CHECK (`from_person_id` <> `to_person_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `person_preferences` (
    `person_id` BIGINT UNSIGNED NOT NULL,
    `dimension` ENUM('licensing', 'performance_permission', 'adaptations', 'attribution', 'commercial_use',
        'recording', 'performance_etiquette', 'objectionable_use') NOT NULL,
    `value_json` JSON NOT NULL,
    `source_url` VARCHAR(2048) NOT NULL,
    PRIMARY KEY (`person_id`, `dimension`),
    CONSTRAINT `fk_person_preferences_person_id` FOREIGN KEY (`person_id`) REFERENCES `persons` (`id`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
