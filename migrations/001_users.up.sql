CREATE TABLE `users` (
    `id` VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    `display_name` VARCHAR(64) NOT NULL,
    `role` ENUM('contributor', 'moderator') NOT NULL DEFAULT 'contributor',
    `created_at` DATETIME NOT NULL,
    `blocked_at` DATETIME NULL,
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
