DROP TABLE `api_tokens`;
DROP TABLE `person_preferences`;
DROP TABLE `piece_preferences`;
ALTER TABLE `persons` DROP FOREIGN KEY `fk_persons_claimed_by_user_id`, DROP COLUMN `claimed_by_user_id`;
ALTER TABLE `revisions`
    MODIFY `source` ENUM('user', 'import') NOT NULL,
    MODIFY `status` ENUM('pending', 'approved', 'rejected', 'withdrawn') NOT NULL;

-- persons lost a column, so recreate the views that expand persons.* (see 009).
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
