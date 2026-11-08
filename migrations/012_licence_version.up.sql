-- The version of a Creative Commons licence a piece is published under. No CHECK ties it to `licence`: the
-- previous release's entity writer does not know the column, so a constraint could fail its writes after a
-- rollback. Reads serve the version only beside a Creative Commons licence instead.
ALTER TABLE `pieces`
    ADD COLUMN `licence_version` ENUM('1.0', '2.0', '2.5', '3.0', '4.0') NULL AFTER `licence`;
