-- Optional second stage: a stronger model writes the report from what the agent's own model collected.
ALTER TABLE agents ADD COLUMN reason_model TEXT;
