CREATE TABLE `terms` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `facet` ENUM('mood', 'topic', 'culture', 'language', 'legendaria', 'form', 'origin', 'other', 'affiliation')
        NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `display_name` VARCHAR(255) NOT NULL,
    `slug` VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `parent_id` BIGINT UNSIGNED NULL,
    -- A topic term that implies an affiliation, such as a kingdom's patriotic songs and the kingdom.
    `related_affiliation_id` BIGINT UNSIGNED NULL,
    `description_md` TEXT NULL,
    `position` INT NOT NULL DEFAULT 0,
    `current_revision_id` BIGINT UNSIGNED NULL,
    `legacy_wiki_pageid` INT UNSIGNED NULL,
    `legacy_title` VARCHAR(255) NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_slug` (`slug`),
    UNIQUE INDEX `idx_facet_name` (`facet`, `name`),
    UNIQUE INDEX `idx_legacy_wiki_pageid` (`legacy_wiki_pageid`),
    INDEX `idx_parent` (`parent_id`),
    INDEX `idx_facet_position_display_name` (`facet`, `position`, `display_name`),
    CONSTRAINT `fk_terms_parent_id` FOREIGN KEY (`parent_id`) REFERENCES `terms` (`id`),
    CONSTRAINT `fk_terms_related_affiliation_id` FOREIGN KEY (`related_affiliation_id`) REFERENCES `terms` (`id`),
    CONSTRAINT `fk_terms_current_revision_id` FOREIGN KEY (`current_revision_id`) REFERENCES `revisions` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `term_aliases` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `term_id` BIGINT UNSIGNED NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_term_name` (`term_id`, `name`),
    CONSTRAINT `fk_term_aliases_term_id` FOREIGN KEY (`term_id`) REFERENCES `terms` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- The closure of terms.parent_id: one row per (ancestor, descendant) pair, including each term with itself at
-- depth 0.
CREATE TABLE `term_ancestors` (
    `ancestor_id` BIGINT UNSIGNED NOT NULL,
    `descendant_id` BIGINT UNSIGNED NOT NULL,
    `depth` TINYINT UNSIGNED NOT NULL,
    PRIMARY KEY (`ancestor_id`, `descendant_id`),
    INDEX `idx_descendant_ancestor` (`descendant_id`, `ancestor_id`),
    CONSTRAINT `fk_term_ancestors_ancestor_id` FOREIGN KEY (`ancestor_id`) REFERENCES `terms` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_term_ancestors_descendant_id` FOREIGN KEY (`descendant_id`) REFERENCES `terms` (`id`)
        ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
