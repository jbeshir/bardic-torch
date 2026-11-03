DROP TABLE `contact_requests`;
DROP TABLE `link_reports`;

ALTER TABLE `users`
    DROP INDEX `idx_display_name_key`,
    DROP COLUMN `display_name_key`;

ALTER TABLE `performances`
    DROP CONSTRAINT `chk_performances_one_owner`,
    DROP FOREIGN KEY `fk_performances_owner_person_id`,
    DROP FOREIGN KEY `fk_performances_owner_piece_id`,
    DROP INDEX `idx_owner_person`,
    DROP INDEX `idx_owner_piece`,
    DROP COLUMN `owner_person_id`,
    DROP COLUMN `owner_piece_id`;

DELETE FROM `revisions` WHERE `status` = 'draft' AND `entity_id` IS NULL;

ALTER TABLE `revisions`
    DROP CONSTRAINT `chk_revisions_entity_unless_draft`,
    DROP FOREIGN KEY `fk_revisions_submitted_base_revision_id`,
    DROP FOREIGN KEY `fk_revisions_rebased_by_user_id`,
    DROP INDEX `idx_author_updated`,
    DROP INDEX `idx_status_submitted`,
    DROP COLUMN `rebased_at`,
    DROP COLUMN `rebased_by_user_id`,
    DROP COLUMN `submitted_at`,
    DROP COLUMN `updated_at`,
    DROP COLUMN `submitted_base_revision_id`,
    DROP COLUMN `received_keys_json`,
    DROP COLUMN `flags_json`,
    DROP COLUMN `submitted_snapshot_json`,
    MODIFY `entity_id` BIGINT UNSIGNED NOT NULL;
