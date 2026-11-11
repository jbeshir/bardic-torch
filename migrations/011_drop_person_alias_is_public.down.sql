ALTER TABLE `person_aliases` ADD COLUMN `is_public` BOOL NOT NULL DEFAULT TRUE AFTER `kind`;
ALTER TABLE `person_aliases` ALTER COLUMN `is_public` SET DEFAULT FALSE;
