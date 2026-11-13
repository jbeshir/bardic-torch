CREATE TABLE `collections` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(255) NOT NULL,
    `sort_name` VARCHAR(255) NOT NULL,
    `sort_letter` CHAR(1) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `url` VARCHAR(2048) NULL,
    `kind` ENUM('index_or_link_list', 'songbook_pdf', 'songbook_website', 'website', 'wiki_category', 'wiki',
        'youtube_channel', 'youtube_playlist', 'google_doc_songbook', 'blog', 'archived_songbook', 'hosted_document')
        NOT NULL,
    `description_md` TEXT NULL,
    `region_term_id` BIGINT UNSIGNED NULL,
    `current_revision_id` BIGINT UNSIGNED NULL,
    PRIMARY KEY (`id`),
    INDEX `idx_sort_letter_name` (`sort_letter`, `sort_name`, `id`),
    INDEX `idx_sort_name` (`sort_name`, `id`),
    CONSTRAINT `fk_collections_region_term_id` FOREIGN KEY (`region_term_id`) REFERENCES `terms` (`id`),
    CONSTRAINT `fk_collections_current_revision_id` FOREIGN KEY (`current_revision_id`) REFERENCES `revisions` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE `events` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `slug` VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `name` VARCHAR(255) NOT NULL,
    `current_revision_id` BIGINT UNSIGNED NULL,
    `legacy_wiki_pageid` INT UNSIGNED NULL,
    `legacy_title` VARCHAR(255) NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_slug` (`slug`),
    UNIQUE INDEX `idx_legacy_wiki_pageid` (`legacy_wiki_pageid`),
    INDEX `idx_name` (`name`, `id`),
    CONSTRAINT `fk_events_current_revision_id` FOREIGN KEY (`current_revision_id`) REFERENCES `revisions` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
