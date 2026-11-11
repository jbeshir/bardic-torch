DROP TABLE `contact_requests`;
DROP TABLE `link_reports`;

ALTER TABLE `pieces` DROP COLUMN `licence_version`;

ALTER TABLE `links`
    DROP INDEX `idx_archive_due`,
    DROP INDEX `idx_check_due`,
    DROP COLUMN `archive_checked_at`,
    DROP COLUMN `liveness_failures`;

ALTER TABLE `performances`
    DROP FOREIGN KEY `fk_performances_owner_person_id`,
    DROP FOREIGN KEY `fk_performances_owner_piece_id`,
    DROP INDEX `idx_owner_person`,
    DROP INDEX `idx_owner_piece`,
    DROP COLUMN `owner_person_id`,
    DROP COLUMN `owner_piece_id`;

-- Revisions of entities that were never created cannot be kept with a NOT NULL entity_id.
DELETE FROM `revisions` WHERE `entity_id` IS NULL;
ALTER TABLE `revisions`
    MODIFY `entity_id` BIGINT UNSIGNED NOT NULL;
