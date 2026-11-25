-- Before 013, data-removal requests were sent as opt_out.
UPDATE `contact_requests` SET `kind` = 'opt_out' WHERE `kind` = 'data_removal';
ALTER TABLE `contact_requests`
    MODIFY `kind` ENUM('takedown', 'opt_out', 'correction', 'link_report', 'general') NOT NULL;
