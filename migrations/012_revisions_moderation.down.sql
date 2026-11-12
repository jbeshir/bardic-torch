CREATE OR REPLACE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `public_link_attachments` AS
SELECT a.*
FROM `link_attachments` a
LEFT JOIN `pieces` p ON p.`id` = a.`piece_id` AND p.`current_revision_id` IS NOT NULL
LEFT JOIN `listed_persons` lp ON lp.`id` = a.`person_id`
WHERE p.`id` IS NOT NULL OR lp.`id` IS NOT NULL;

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
