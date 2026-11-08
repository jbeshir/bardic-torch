ALTER TABLE `link_reports`
    DROP INDEX `idx_reporter_link_status`;

ALTER TABLE `links`
    DROP INDEX `idx_archive_due`,
    DROP INDEX `idx_check_due`,
    DROP COLUMN `archive_checked_at`,
    DROP COLUMN `liveness_failures`;

DROP TABLE `repertoire`;

ALTER TABLE `users`
    DROP INDEX `idx_public_id`,
    DROP COLUMN `public_id`;
