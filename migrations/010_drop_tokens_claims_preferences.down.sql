ALTER TABLE `revisions`
    MODIFY `source` ENUM('user', 'import', 'mcp') NOT NULL,
    MODIFY `status` ENUM('draft', 'pending', 'approved', 'rejected', 'withdrawn') NOT NULL;
ALTER TABLE `persons`
    ADD COLUMN `claimed_by_user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL AFTER `is_stub`,
    ADD CONSTRAINT `fk_persons_claimed_by_user_id` FOREIGN KEY (`claimed_by_user_id`) REFERENCES `users` (`id`);

CREATE TABLE `api_tokens` (
    `id` CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    `token_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `token_prefix` CHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `name` VARCHAR(128) NULL,
    `created_at` DATETIME NOT NULL,
    `last_used_at` DATETIME NULL,
    `revoked_at` DATETIME NULL,
    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_token_hash` (`token_hash`),
    INDEX `idx_user_revoked` (`user_id`, `revoked_at`),
    CONSTRAINT `fk_api_tokens_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE
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

CREATE TABLE `piece_preferences` (
    `piece_id` BIGINT UNSIGNED NOT NULL,
    `dimension` ENUM('licensing', 'performance_permission', 'adaptations', 'attribution', 'commercial_use',
        'recording', 'performance_etiquette', 'objectionable_use') NOT NULL,
    `value_json` JSON NOT NULL,
    `source_url` VARCHAR(2048) NOT NULL,
    PRIMARY KEY (`piece_id`, `dimension`),
    CONSTRAINT `fk_piece_preferences_piece_id` FOREIGN KEY (`piece_id`) REFERENCES `pieces` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Recreate the views so that persons.* expands to the restored column.
CREATE OR REPLACE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `listed_persons` AS
SELECT p.*
FROM `persons` p
WHERE p.`current_revision_id` IS NOT NULL AND p.`status` <> 'prefers_no_page';

CREATE OR REPLACE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `public_link_attachments` AS
SELECT a.*
FROM `link_attachments` a
LEFT JOIN `pieces` p ON p.`id` = a.`piece_id` AND p.`current_revision_id` IS NOT NULL
LEFT JOIN `listed_persons` lp ON lp.`id` = a.`person_id`
WHERE p.`id` IS NOT NULL OR lp.`id` IS NOT NULL;
