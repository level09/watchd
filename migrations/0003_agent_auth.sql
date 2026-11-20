-- Optional API credential: JSON { host, secret }, where secret names a Worker secret sent as a Bearer token to that host only.
ALTER TABLE agents ADD COLUMN auth TEXT;
