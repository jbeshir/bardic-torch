-- The visibility rules every public read shares, defined once. Queries join these views instead of restating the
-- rules; MERGE makes MySQL inline each view into the query using it, so a view costs what its predicate would.
-- SQL SECURITY INVOKER checks the reading user's privileges, so the views do not depend on the migrating user
-- still existing.
--
-- MySQL expands a view's * when the view is created, so a column added to terms, persons or link_attachments
-- later is not in these views until a migration recreates them (CREATE OR REPLACE VIEW). Dropping or renaming a
-- column a view selects still succeeds, but the view is then invalid and every read using it fails (error 1356),
-- so a migration that drops or renames such a column must recreate the views without it in the same migration.

-- A public term: approved, with every ancestor approved. The closure's depth-0 row covers the term itself.
CREATE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `public_terms` AS
SELECT t.*
FROM `terms` t
WHERE NOT EXISTS (
    SELECT 1
    FROM `term_ancestors` ta
    JOIN `terms` anc ON anc.`id` = ta.`ancestor_id`
    WHERE ta.`descendant_id` = t.`id` AND anc.`current_revision_id` IS NULL
);

-- A public person with a page: approved, and not preferring no page. Persons who prefer no page are public but
-- only as an identity; they are left out of every list that references persons.
CREATE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `listed_persons` AS
SELECT p.*
FROM `persons` p
WHERE p.`current_revision_id` IS NOT NULL AND p.`status` <> 'prefers_no_page';

-- A public attachment of a link: to a public piece, or to a public person with a page. What a link records, such
-- as its performances, may be shown on another entity's page only if the link has a public attachment, as a
-- link attached only to an unapproved or no-page entity may name them.
CREATE ALGORITHM = MERGE SQL SECURITY INVOKER VIEW `public_link_attachments` AS
SELECT a.*
FROM `link_attachments` a
LEFT JOIN `pieces` p ON p.`id` = a.`piece_id` AND p.`current_revision_id` IS NOT NULL
LEFT JOIN `listed_persons` lp ON lp.`id` = a.`person_id`
WHERE p.`id` IS NOT NULL OR lp.`id` IS NOT NULL;
