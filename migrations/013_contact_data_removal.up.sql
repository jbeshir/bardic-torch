ALTER TABLE `contact_requests`
    MODIFY `kind` ENUM('takedown', 'opt_out', 'correction', 'link_report', 'general', 'data_removal') NOT NULL;
