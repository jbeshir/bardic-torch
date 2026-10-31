CREATE TABLE `api_tokens` (
    `id` CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `user_id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
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
